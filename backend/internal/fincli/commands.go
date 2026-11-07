package fincli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/x2db736ac85h/finuance/backend/internal/client/gen"
)

type command func(ctx context.Context, c *cli, args []string) error

var commands = map[string]command{
	"login":        cmdLogin,
	"whoami":       cmdWhoami,
	"accounts":     cmdAccounts,
	"transactions": cmdTransactions,
	"import":       cmdImport,
	"overview":     cmdOverview,
	"groups":       cmdGroups,
	"shared":       cmdShared,
	"budgets":      cmdBudgets,
	"goals":        cmdGoals,
	"ask":          cmdAsk,
}

func flags(c *cli, name string) *flag.FlagSet {
	fs := flag.NewFlagSet("fin "+name, flag.ContinueOnError)
	fs.SetOutput(c.env.Stderr)
	return fs
}

func parse(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		return usageError{err.Error()}
	}
	return nil
}

// cmdLogin stores the address and a token read from stdin (never from the command line, where it
// would stay in the shell's history), after checking them with GET /me.
func cmdLogin(ctx context.Context, c *cli, args []string) error {
	fs := flags(c, "login")
	url := fs.String("url", c.url, "the instance's address, e.g. https://finance.example.com")
	if err := parse(fs, args); err != nil {
		return err
	}
	if *url == "" {
		return usageError{"login needs --url"}
	}
	fmt.Fprint(c.env.Stderr, "Access token (Settings → Access tokens): ")
	line, err := bufio.NewReader(c.env.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	fmt.Fprintln(c.env.Stderr)
	c.url, c.token = strings.TrimRight(*url, "/"), strings.TrimSpace(line)
	if !strings.HasPrefix(c.token, "fin_") {
		return errors.New("that is not an access token (they start with fin_)")
	}
	if err := c.newClient(); err != nil {
		return err
	}
	me, err := c.api.GetMeWithResponse(ctx)
	if err != nil {
		return err
	}
	if err := check(me.StatusCode(), me.Body, http.StatusOK); err != nil {
		return err
	}
	p, err := c.writeConfig(Config{URL: c.url, Token: c.token})
	if err != nil {
		return err
	}
	fmt.Fprintf(c.env.Stdout, "Signed in to %s as %s (saved in %s)\n", c.url, me.JSON200.Email, p)
	return nil
}

func cmdWhoami(ctx context.Context, c *cli, _ []string) error {
	me, err := c.api.GetMeWithResponse(ctx)
	if err != nil {
		return err
	}
	if err := check(me.StatusCode(), me.Body, http.StatusOK); err != nil {
		return err
	}
	u := me.JSON200
	return c.print(table{head: []string{"EMAIL", "NAME", "URL"}, rows: [][]string{{u.Email, u.DisplayName, c.url}}, raw: u})
}

func (c *cli) accounts(ctx context.Context) ([]gen.Account, error) {
	r, err := c.api.ListAccountsWithResponse(ctx)
	if err != nil {
		return nil, err
	}
	if err := check(r.StatusCode(), r.Body, http.StatusOK); err != nil {
		return nil, err
	}
	return *r.JSON200, nil
}

// account finds an account by id, or by name ignoring case.
func (c *cli) account(ctx context.Context, ref string) (*gen.Account, error) {
	list, err := c.accounts(ctx)
	if err != nil {
		return nil, err
	}
	var names []string
	for i := range list {
		if list[i].Id.String() == ref || strings.EqualFold(list[i].Name, ref) {
			return &list[i], nil
		}
		names = append(names, list[i].Name)
	}
	return nil, fmt.Errorf("no account %q (yours: %s)", ref, strings.Join(names, ", "))
}

func dateStr(d *openapi_types.Date) string {
	if d == nil {
		return ""
	}
	return d.String()
}

func cmdAccounts(ctx context.Context, c *cli, _ []string) error {
	list, err := c.accounts(ctx)
	if err != nil {
		return err
	}
	t := table{head: []string{"NAME", "KIND", "CURRENCY", "TRANSACTIONS", "FIRST", "LAST", "ID"}, raw: list}
	for i := range list {
		a := &list[i]
		t.rows = append(t.rows, []string{a.Name, string(a.Kind), a.Currency, strconv.FormatInt(a.TxCount, 10), dateStr(a.FirstOn),
			dateStr(a.LastOn), a.Id.String()})
	}
	return c.print(t)
}

// queryContext builds the filters the transactions command shares with the export.
type txFlags struct {
	account, search, category, payee, month, from, to string
}

func (f *txFlags) add(fs *flag.FlagSet) {
	fs.StringVar(&f.account, "account", "", "an account's name or id (default every account)")
	fs.StringVar(&f.search, "search", "", "words to search for, as in the table's search box")
	fs.StringVar(&f.category, "category", "", "a category's name or part of it")
	fs.StringVar(&f.payee, "payee", "", "text in the payee")
	fs.StringVar(&f.month, "month", "", "a month, YYYY-MM")
	fs.StringVar(&f.from, "from", "", "from this day, YYYY-MM-DD")
	fs.StringVar(&f.to, "to", "", "up to and including this day, YYYY-MM-DD")
}

func (f *txFlags) context(ctx context.Context, c *cli) (gen.QueryContext, error) {
	var q gen.QueryContext
	if f.account != "" {
		a, err := c.account(ctx, f.account)
		if err != nil {
			return q, err
		}
		q.AccountIds = &[]openapi_types.UUID{a.Id}
	}
	if f.search != "" {
		q.Search = &f.search
	}
	if f.category != "" || f.payee != "" {
		tf := gen.ToolFilter{}
		if f.category != "" {
			tf.Category = &f.category
		}
		if f.payee != "" {
			tf.Payee = &f.payee
		}
		q.ToolFilter = &tf
	}
	switch {
	case f.month != "" && (f.from != "" || f.to != ""):
		return q, usageError{"give --month or --from/--to, not both"}
	case f.month != "":
		q.Period = &gen.Period{Month: &f.month}
	case f.from != "" || f.to != "":
		p := gen.Period{}
		if f.from != "" {
			d, err := time.Parse(time.DateOnly, f.from)
			if err != nil {
				return q, usageError{"--from must be YYYY-MM-DD"}
			}
			p.From = &openapi_types.Date{Time: d}
		}
		if f.to != "" {
			d, err := time.Parse(time.DateOnly, f.to)
			if err != nil {
				return q, usageError{"--to must be YYYY-MM-DD"}
			}
			p.To = &openapi_types.Date{Time: d.AddDate(0, 0, 1)} // the API's end is exclusive
		}
		q.Period = &p
	}
	return q, nil
}

func cmdTransactions(ctx context.Context, c *cli, args []string) error {
	fs := flags(c, "transactions")
	var f txFlags
	f.add(fs)
	limit := fs.Int("limit", 50, "rows to print (at most 1000)")
	export := fs.String("export", "", "write the bank-style CSV export of the filtered rows to this file (- for stdout)")
	if err := parse(fs, args); err != nil {
		return err
	}
	q, err := f.context(ctx, c)
	if err != nil {
		return err
	}
	if *export != "" {
		return c.export(ctx, q, *export)
	}
	if *limit < 1 || *limit > 1000 {
		return usageError{"--limit must be 1 to 1000"}
	}
	r, err := c.api.QueryTransactionsWithResponse(ctx, gen.QueryTransactionsJSONRequestBody{Context: q, Limit: *limit})
	if err != nil {
		return err
	}
	if err := check(r.StatusCode(), r.Body, http.StatusOK); err != nil {
		return err
	}
	page := r.JSON200
	t := table{head: []string{"DATE", "AMOUNT", "PAYEE", "CATEGORY", "ACCOUNT", "NOTE"}, raw: page}
	for i := range page.Rows {
		row := &page.Rows[i]
		cat := ""
		if row.Category != nil {
			cat = row.Category.Name
		}
		t.rows = append(t.rows, []string{row.BookedOn.String(), amount(row.Amount.AmountMinor, row.Amount.Currency), row.Payee, cat,
			row.AccountName, row.Note})
	}
	if err := c.print(t); err != nil {
		return err
	}
	if c.out == "table" && page.Total != nil && *page.Total > len(page.Rows) {
		fmt.Fprintf(c.env.Stderr, "%d of %d rows; --limit shows more\n", len(page.Rows), *page.Total)
	}
	return nil
}

var exportColumns = []gen.FilterColumn{gen.FilterColumnBookedOn, gen.FilterColumnAmount, gen.FilterColumnCurrency, gen.FilterColumnPayee,
	gen.FilterColumnDescription, gen.FilterColumnCategory, gen.FilterColumnAccount, gen.FilterColumnGroup, gen.FilterColumnNote}

func (c *cli) export(ctx context.Context, q gen.QueryContext, to string) error {
	r, err := c.api.ExportTransactionsWithResponse(ctx, gen.ExportTransactionsJSONRequestBody{Columns: exportColumns, Context: q})
	if err != nil {
		return err
	}
	if err := check(r.StatusCode(), r.Body, http.StatusOK); err != nil {
		return err
	}
	if to == "-" {
		_, err := c.env.Stdout.Write(r.Body)
		return err
	}
	if err := os.WriteFile(to, r.Body, 0o600); err != nil {
		return err
	}
	fmt.Fprintf(c.env.Stderr, "wrote %s (%d bytes)\n", to, len(r.Body))
	return nil
}

// cmdImport uploads a CSV, shows the preview and, with --commit, imports it and waits for the job.
func cmdImport(ctx context.Context, c *cli, args []string) error {
	fs := flags(c, "import")
	accountRef := fs.String("account", "", "the account it belongs to (name or id); default the one the file names")
	commit := fs.Bool("commit", false, "import it (otherwise only the preview)")
	if err := parse(fs, reorder(args)); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return usageError{"import needs one CSV file"}
	}
	file := fs.Arg(0)
	content, err := os.ReadFile(file) //nolint:gosec // the file the user named
	if err != nil {
		return err
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if *accountRef != "" {
		a, err := c.account(ctx, *accountRef)
		if err != nil {
			return err
		}
		_ = mw.WriteField("accountId", a.Id.String())
	}
	part, err := mw.CreateFormFile("file", filepath.Base(file))
	if err != nil {
		return err
	}
	_, _ = part.Write(content)
	if err := mw.Close(); err != nil {
		return err
	}
	up, err := c.api.UploadImportWithBodyWithResponse(ctx, &gen.UploadImportParams{}, mw.FormDataContentType(), &body)
	if err != nil {
		return err
	}
	if err := check(up.StatusCode(), up.Body, http.StatusCreated, http.StatusAccepted); err != nil {
		return err
	}
	pv := up.JSON201
	if pv == nil {
		pv = up.JSON202
	}
	st := pv.Stats
	fmt.Fprintf(c.env.Stdout, "%s: %d rows, %d with errors, about %d new and %d already imported", filepath.Base(file), st.Rows, st.Errors,
		st.NewEstimate, st.DuplicatesEstimate)
	if st.DateFrom != nil && st.DateTo != nil {
		fmt.Fprintf(c.env.Stdout, ", %s to %s", st.DateFrom, st.DateTo)
	}
	fmt.Fprintln(c.env.Stdout)
	id := pv.Import.Id
	if len(pv.Missing) > 0 {
		return fmt.Errorf("the file's columns for %s weren't found; map them in the import window (/imports/%s)", strings.Join(pv.Missing, ", "), id)
	}
	if up.StatusCode() == http.StatusAccepted { // the fast path committed it already
		return c.waitImport(ctx, id)
	}
	if !*commit {
		fmt.Fprintf(c.env.Stdout, "Not imported yet: run again with --commit, or open /imports/%s\n", id)
		return nil
	}
	if pv.Import.AccountId == nil {
		return errors.New("the file names no account: give --account")
	}
	cr, err := c.api.CommitImportWithResponse(ctx, id, gen.CommitImportJSONRequestBody{})
	if err != nil {
		return err
	}
	if err := check(cr.StatusCode(), cr.Body, http.StatusAccepted); err != nil {
		return err
	}
	return c.waitImport(ctx, id)
}

// reorder puts flags before the file name, so "fin import file.csv --commit" works too.
func reorder(args []string) []string {
	var fl, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case strings.HasPrefix(a, "-"):
			fl = append(fl, a)
			if !strings.Contains(a, "=") && a != "--commit" && a != "-commit" && i+1 < len(args) {
				fl = append(fl, args[i+1])
				i++
			}
		default:
			pos = append(pos, a)
		}
	}
	return append(fl, pos...)
}

func (c *cli) waitImport(ctx context.Context, id uuid.UUID) error {
	for range 240 { // two minutes
		r, err := c.api.GetImportWithResponse(ctx, id)
		if err != nil {
			return err
		}
		if err := check(r.StatusCode(), r.Body, http.StatusOK); err != nil {
			return err
		}
		d := r.JSON200
		switch d.Status {
		case gen.ImportDetailStatusCommitted:
			fmt.Fprintf(c.env.Stdout, "Imported: %v new, %v already there\n", stat(d.Stats, "new"), stat(d.Stats, "duplicates"))
			return nil
		case gen.ImportDetailStatusFailed:
			msg := "the import failed"
			if d.Error != nil {
				msg += ": " + *d.Error
			}
			return errors.New(msg)
		}
		c.env.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("the import is still running; see /imports/%s", id)
}

func stat(m map[string]any, k string) any {
	if v, ok := m[k]; ok {
		return v
	}
	return 0
}

func cmdOverview(ctx context.Context, c *cli, args []string) error {
	fs := flags(c, "overview")
	month := fs.String("month", "", "YYYY-MM (default the latest month with data)")
	accountRef := fs.String("account", "", "one account (name or id)")
	if err := parse(fs, args); err != nil {
		return err
	}
	body := gen.OverviewJSONRequestBody{Months: 6} // the cash-flow cards' span (6, 12, 24 or 36); fin prints the month itself
	if *month != "" {
		body.Month = month
	}
	if *accountRef != "" {
		a, err := c.account(ctx, *accountRef)
		if err != nil {
			return err
		}
		body.Context.AccountIds = &[]openapi_types.UUID{a.Id}
	}
	r, err := c.api.OverviewWithResponse(ctx, body)
	if err != nil {
		return err
	}
	if err := check(r.StatusCode(), r.Body, http.StatusOK); err != nil {
		return err
	}
	o := r.JSON200
	if o.Data == nil {
		fmt.Fprintln(c.env.Stdout, "No transactions yet.")
		return nil
	}
	d, cur := o.Data, o.Currency
	t := table{head: []string{"CATEGORY", "SPENT", "BUDGET", "STATUS"}, raw: o}
	for i := range d.Categories {
		b := &d.Categories[i]
		budget := amount(b.BudgetMinor, cur)
		if b.BudgetIsDefault {
			budget += " (default)"
		}
		t.rows = append(t.rows, []string{b.Name, amount(b.CurMinor, cur), budget, string(b.Status)})
	}
	if c.out == "table" {
		partial := ""
		if d.Partial {
			partial = fmt.Sprintf(" (day %d of %d)", d.ElapsedDays, d.DaysInMonth)
		}
		fmt.Fprintf(c.env.Stdout, "%s%s: income %s, spent %s, net %s, %d transactions\n\n", d.Month, partial,
			amount(d.Current.IncomeMinor, cur), amount(d.Current.ExpenseMinor, cur), amount(d.Current.IncomeMinor-d.Current.ExpenseMinor, cur),
			d.Current.Count)
	}
	return c.print(t)
}

func cmdGroups(ctx context.Context, c *cli, _ []string) error {
	r, err := c.api.ListGroupsWithResponse(ctx)
	if err != nil {
		return err
	}
	if err := check(r.StatusCode(), r.Body, http.StatusOK); err != nil {
		return err
	}
	t := table{head: []string{"NAME", "TRANSACTIONS", "SPENT", "INCOME", "FIRST", "LAST", "SHARED"}, raw: r.JSON200}
	groups := *r.JSON200
	for i := range groups {
		g := &groups[i]
		shared := ""
		if g.SharedGroupId != nil {
			shared = "yes"
		}
		t.rows = append(t.rows, []string{g.Name, strconv.Itoa(g.Count), amount(g.Spent.AmountMinor, g.Spent.Currency),
			amount(g.Income.AmountMinor, g.Income.Currency), dateStr(g.FirstOn), dateStr(g.LastOn), shared})
	}
	return c.print(t)
}

func cmdShared(ctx context.Context, c *cli, _ []string) error {
	r, err := c.api.ListSharedGroupsWithResponse(ctx)
	if err != nil {
		return err
	}
	if err := check(r.StatusCode(), r.Body, http.StatusOK); err != nil {
		return err
	}
	t := table{head: []string{"NAME", "KIND", "CURRENCY", "FROM", "TO", "ARCHIVED", "ID"}, raw: r.JSON200}
	groups := *r.JSON200
	for i := range groups {
		g := &groups[i]
		archived := ""
		if g.Archived {
			archived = "yes"
		}
		t.rows = append(t.rows, []string{g.Name, string(g.Kind), g.Currency, dateStr(g.StartsOn), dateStr(g.EndsOn), archived,
			g.Id.String()})
	}
	return c.print(t)
}

func cmdBudgets(ctx context.Context, c *cli, _ []string) error {
	r, err := c.api.ListBudgetsWithResponse(ctx)
	if err != nil {
		return err
	}
	if err := check(r.StatusCode(), r.Body, http.StatusOK); err != nil {
		return err
	}
	t := table{head: []string{"CATEGORY", "MONTHLY BUDGET"}, raw: r.JSON200}
	for _, b := range r.JSON200.Budgets {
		t.rows = append(t.rows, []string{b.Category.Name, amount(b.AmountMinor, "")})
	}
	return c.print(t)
}

func cmdGoals(ctx context.Context, c *cli, _ []string) error {
	r, err := c.api.ListGoalsWithResponse(ctx)
	if err != nil {
		return err
	}
	if err := check(r.StatusCode(), r.Body, http.StatusOK); err != nil {
		return err
	}
	t := table{head: []string{"NAME", "KIND", "STATE", "PROGRESS", "DEADLINE", "STATUS"}, raw: r.JSON200}
	for i := range r.JSON200.Goals {
		g := &r.JSON200.Goals[i]
		t.rows = append(t.rows, []string{g.Name, string(g.Kind), string(g.Progress.State), fmt.Sprintf("%d%%", g.Progress.Percent),
			g.Deadline.String(), string(g.Status)})
	}
	return c.print(t)
}

// cmdAsk asks the assistant in a new chat and prints the answer as it streams; lookups and
// proposals are listed on stderr (apply proposals in the app).
func cmdAsk(ctx context.Context, c *cli, args []string) error {
	fs := flags(c, "ask")
	model := fs.String("model", "", "a model of the default connection (default the instance's)")
	if err := parse(fs, args); err != nil {
		return err
	}
	q := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if q == "" {
		return usageError{`ask needs a question, e.g. fin ask "What did I spend on groceries last month?"`}
	}
	chat, err := c.api.CreateAssistantChatWithResponse(ctx)
	if err != nil {
		return err
	}
	if err := check(chat.StatusCode(), chat.Body, http.StatusCreated); err != nil {
		return err
	}
	tz := time.Local.String()
	body := gen.SendAssistantMessageJSONRequestBody{Text: q}
	if tz != "" && tz != "Local" {
		body.TimeZone = &tz
	}
	if *model != "" {
		body.Settings = &gen.AssistantRunSettings{Model: model}
	}
	res, err := c.stream.SendAssistantMessage(ctx, chat.JSON201.Id, body)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		return apiError(res.StatusCode, b)
	}
	return c.readAnswer(res.Body)
}

// readAnswer prints a run's server-sent events: the answer to stdout, lookups to stderr.
func (c *cli) readAnswer(r io.Reader) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	event := ""
	proposals := 0
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data := []byte(strings.TrimPrefix(line, "data: "))
			switch event {
			case "content":
				var d gen.AssistantDeltaEvent
				if json.Unmarshal(data, &d) == nil {
					fmt.Fprint(c.env.Stdout, d.Delta)
				}
			case "step":
				var s struct {
					Tool     string          `json:"tool"`
					Summary  string          `json:"summary"`
					Proposal json.RawMessage `json:"proposal"`
				}
				if json.Unmarshal(data, &s) == nil {
					fmt.Fprintf(c.env.Stderr, "· %s: %s\n", s.Tool, s.Summary)
					if len(s.Proposal) > 0 && string(s.Proposal) != "null" {
						proposals++
					}
				}
			case "error":
				var e gen.AssistantErrorEvent
				_ = json.Unmarshal(data, &e)
				fmt.Fprintln(c.env.Stdout)
				return fmt.Errorf("the assistant couldn't answer: %s", e.Message)
			case "done":
				fmt.Fprintln(c.env.Stdout)
				if proposals > 0 {
					fmt.Fprintf(c.env.Stderr, "%d proposed change(s): open the chat in the app to apply them\n", proposals)
				}
				return nil
			}
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return errors.New("the answer ended early")
}
