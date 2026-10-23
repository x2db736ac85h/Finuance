package legacyimport

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/ingest"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/money"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// Manifest is the manifest part of POST /legacy-imports: what the browser read from the backup
// (decrypted there; the passphrase never reaches the server) and what the user chose in the
// wizard. The file bytes travel as separate parts, in the order of Files.
type Manifest struct {
	Version     int            `json:"version"`
	Source      string         `json:"source"` // finuanceBackup | csvInspectorBackup
	Created     string         `json:"created,omitempty"`
	Opts        ParseOpts      `json:"opts"`
	Files       []ManifestFile `json:"files"`
	Private     Private        `json:"private"`
	Opening     *AccountRef    `json:"opening,omitempty"` // the account that receives the legacy opening balance
	Chats       []Chat         `json:"chats,omitempty"`   // nil: chats are not migrated
	UnknownKeys []string       `json:"unknownKeys,omitempty"`
}

// ManifestFile is one legacy file record and the account it goes to.
type ManifestFile struct {
	LegacyID string     `json:"legacyId"`
	Name     string     `json:"name"`
	Added    int64      `json:"added"` // ms; the legacy app ordered its files by it
	Cfg      *FileCfg   `json:"cfg,omitempty"`
	Account  AccountRef `json:"account"`
}

// AccountRef is an existing account or one to create. A new account with the name of an existing
// one uses that account, so running the migration again creates nothing new.
type AccountRef struct {
	ID  *uuid.UUID  `json:"id,omitempty"`
	New *NewAccount `json:"new,omitempty"`
}

// NewAccount is an account the migration creates.
type NewAccount struct {
	Name     string `json:"name"`
	Currency string `json:"currency,omitempty"` // "" = PLN
}

func (a AccountRef) key() string {
	if a.ID != nil {
		return "id:" + a.ID.String()
	}
	if a.New != nil {
		return "new:" + strings.ToLower(strings.TrimSpace(a.New.Name))
	}
	return ""
}

// Private is the legacy kv.private (decrypted): budgets, opening balance, groups and labels,
// duplicate decisions and investments. The legacy AI key is never sent.
type Private struct {
	Budgets map[string]Num             `json:"budgets,omitempty"`
	Opening Num                        `json:"opening,omitempty"`
	Groups  []Group                    `json:"groups,omitempty"`
	Tags    map[string]string          `json:"tags,omitempty"`    // legacy key → group id
	DupKeep map[string]json.RawMessage `json:"dupKeep,omitempty"` // key → truthy
	DupPick map[string]string          `json:"dupPick,omitempty"` // key → legacy file id
	Inv     *Inv                       `json:"inv,omitempty"`
}

// Group is a legacy group.
type Group struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color int    `json:"color"`
}

// Inv is the legacy investments document (S.inv): amounts in major units.
type Inv struct {
	PPK struct {
		Employee Num `json:"employee"`
		Employer Num `json:"employer"`
		State    Num `json:"state"`
		Value    Num `json:"value"`
	} `json:"ppk"`
	Positions []struct {
		Name     string `json:"name"`
		Type     string `json:"type"`
		Invested Num    `json:"invested"`
		Value    Num    `json:"value"`
		Income   Num    `json:"income"`
	} `json:"positions"`
	Swr Num `json:"swr"` // percent
	Ret Num `json:"ret"` // percent
}

// Chat is a legacy chat (S.chats): its turns as the panel showed them.
type Chat struct {
	ID      string `json:"id"`
	Created int64  `json:"created"`
	Updated int64  `json:"updated"`
	Title   string `json:"title"`
	Turns   []Turn `json:"turns"`
}

// Turn is a legacy chat turn: a question (role "user", text) or an answer (role "ai").
type Turn struct {
	Role      string `json:"role"`
	Text      string `json:"text,omitempty"`
	Content   string `json:"content,omitempty"`
	Reasoning string `json:"reasoning,omitempty"`
	Error     string `json:"error,omitempty"`
	Meta      struct {
		Model string `json:"model,omitempty"`
	} `json:"meta"`
	Steps []struct {
		Name    string          `json:"name"`
		Args    json.RawMessage `json:"args,omitempty"`
		Summary string          `json:"summary,omitempty"`
		JSON    string          `json:"json,omitempty"`
	} `json:"steps,omitempty"`
}

// Num is a legacy number: a JSON number, a numeric string, or null/empty (zero). It keeps the
// decimal text, so amounts never pass through float64.
type Num string

// UnmarshalJSON accepts a number, a string or null.
func (n *Num) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	switch {
	case string(b) == "null":
		*n = ""
	case len(b) > 0 && b[0] == '"':
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*n = Num(strings.TrimSpace(s))
	default:
		var x json.Number
		if err := json.Unmarshal(b, &x); err != nil {
			return err
		}
		*n = Num(x.String())
	}
	return nil
}

var reExponent = regexp.MustCompile(`[eE]`)

// minor is the value in minor units of a two-decimal currency, the only kind the legacy app knew
// ("" and invalid values are 0; ok is false for an invalid one).
func (n Num) minor() (int64, bool) {
	if n == "" || reExponent.MatchString(string(n)) {
		return 0, n == ""
	}
	v, err := money.ParseMinor(string(n), 2)
	return v, err == nil
}

// Limits of a manifest.
const (
	MaxFiles   = 200
	maxTags    = 500_000
	maxGroups  = 1000
	maxChats   = 200
	maxTurns   = 400
	maxBudgets = 2000
	maxKeyLen  = 2000
)

// Validate checks the manifest's shape and limits (422 on failure).
func (m *Manifest) Validate() error {
	ve := &platform.ValidationError{}
	if m.Version != 1 {
		ve.Add("manifest.version", "must be 1")
	}
	if m.Source != "finuanceBackup" && m.Source != "csvInspectorBackup" {
		ve.Add("manifest.source", "must be finuanceBackup or csvInspectorBackup")
	}
	if len(m.Files) == 0 || len(m.Files) > MaxFiles {
		ve.Add("manifest.files", fmt.Sprintf("must hold 1–%d files", MaxFiles))
	}
	ids := map[string]bool{}
	for i, f := range m.Files {
		p := fmt.Sprintf("manifest.files[%d]", i)
		if f.LegacyID == "" || len(f.LegacyID) > 64 || ids[f.LegacyID] {
			ve.Add(p+".legacyId", "must be a unique id of 1–64 characters")
		}
		ids[f.LegacyID] = true
		if f.Name == "" || utf8.RuneCountInString(f.Name) > 255 {
			ve.Add(p+".name", "must have 1–255 characters")
		}
		checkAccount(ve, p+".account", f.Account)
	}
	if m.Opening != nil {
		checkAccount(ve, "manifest.opening", *m.Opening)
	}
	pr := &m.Private
	switch {
	case len(pr.Tags) > maxTags:
		ve.Add("manifest.private.tags", fmt.Sprintf("at most %d labels", maxTags))
	case len(pr.Groups) > maxGroups:
		ve.Add("manifest.private.groups", fmt.Sprintf("at most %d groups", maxGroups))
	case len(pr.Budgets) > maxBudgets:
		ve.Add("manifest.private.budgets", fmt.Sprintf("at most %d budgets", maxBudgets))
	case len(m.Chats) > maxChats:
		ve.Add("manifest.chats", fmt.Sprintf("at most %d chats", maxChats))
	}
	for k := range pr.Tags {
		if len(k) > maxKeyLen {
			ve.Add("manifest.private.tags", "a label key is too long")
			break
		}
	}
	for i, c := range m.Chats {
		if len(c.Turns) > maxTurns {
			ve.Add(fmt.Sprintf("manifest.chats[%d].turns", i), fmt.Sprintf("at most %d turns", maxTurns))
		}
	}
	if len(m.UnknownKeys) > 200 {
		m.UnknownKeys = m.UnknownKeys[:200]
	}
	return ve.Err()
}

func checkAccount(ve *platform.ValidationError, field string, a AccountRef) {
	switch {
	case (a.ID == nil) == (a.New == nil):
		ve.Add(field, "must name an existing account (id) or a new one (new)")
	case a.New != nil:
		if n := utf8.RuneCountInString(strings.TrimSpace(a.New.Name)); n < 1 || n > 80 {
			ve.Add(field+".new.name", "must have 1–80 characters")
		}
		if c := a.New.Currency; c != "" && !regexp.MustCompile(`^[A-Z]{3}$`).MatchString(c) {
			ve.Add(field+".new.currency", "must be an ISO 4217 code")
		}
	}
}

// sortedFiles returns the indices of the manifest files in the legacy order: by added, then by id
// (IndexedDB returned the records by id, and the legacy sort was stable).
func (m *Manifest) sortedFiles() []int {
	order := make([]int, len(m.Files))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		fa, fb := m.Files[a], m.Files[b]
		switch {
		case fa.Added != fb.Added:
			return cmpInt(fa.Added, fb.Added)
		default:
			return strings.Compare(fa.LegacyID, fb.LegacyID)
		}
	})
	return order
}

func cmpInt(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// importOptions translates the legacy settings of a file (e.cfg over the global options) into
// the reading options of an import.
func importOptions(cfg *FileCfg, g ParseOpts) ledger.OptionsDoc {
	c := FileCfg{}
	if cfg != nil {
		c = *cfg
	}
	fo := optsOf(c, g)
	o := ledger.DefaultOptions()
	o.Encoding, o.Delimiter, o.StripQuote = fo.enc, fo.delim, fo.strip
	if o.Delimiter == "\t" {
		o.Delimiter = `\t`
	}
	switch {
	case fo.header == nil:
	case *fo.header == 0:
		o.HeaderRow, o.SkipRows = ledger.HeaderRowOpt{Mode: ingest.HeaderNone}, fo.skip
	default:
		o.HeaderRow = ledger.HeaderRowOpt{Mode: ingest.HeaderRow, Row: *fo.header}
	}
	o.DropShort, o.InvertSign = fo.dropShort, fo.invert
	if fo.mdy {
		o.DateOrder = "mdy"
	}
	return o
}

// applyRoles puts the legacy role map (column → role) over a mapping: each mapped column takes
// its role (and loses any other), "ignore" ignores it, and debit + credit without a signed amount
// switch the import to two amount columns, as the legacy applyMapping did. Columns past the
// headers are skipped.
func applyRoles(base ledger.MappingDoc, opts *ledger.OptionsDoc, cfg *FileCfg, headers []string) ledger.MappingDoc {
	if cfg == nil || len(cfg.Map) == 0 {
		return base
	}
	roles := optsOf(*cfg, ParseOpts{}).roles
	cols := make([]int, 0, len(roles))
	hasAmount := false
	for j, role := range roles {
		if j < len(headers) {
			cols = append(cols, j)
			hasAmount = hasAmount || role == "amount"
		}
	}
	slices.Sort(cols)
	m := base
	fields := []**ledger.ColRef{&m.BookedOn, &m.Amount, &m.Payee, &m.Description, &m.Reference, &m.Category, &m.Currency,
		&m.ValueOn, &m.TxType, &m.CounterpartyAccount, &m.OwnAccount, &m.Balance}
	for _, j := range cols {
		for _, f := range fields {
			if *f != nil && (*f).Index == j {
				*f = nil
			}
		}
		m.Ignore = slices.DeleteFunc(slices.Clone(m.Ignore), func(c ledger.ColRef) bool { return c.Index == j })
	}
	for _, j := range cols {
		ref := &ledger.ColRef{Index: j, Header: headers[j]}
		switch roles[j] {
		case "date":
			m.BookedOn = ref
		case "vdate":
			m.ValueOn = ref
		case "amount":
			m.Amount = ref
		case "payee":
			m.Payee = ref
		case "desc":
			m.Description = ref
		case "category":
			m.Category = ref
		case "currency":
			m.Currency = ref
		case "ref":
			m.Reference = ref
		case "type":
			m.TxType = ref
		case "account":
			m.OwnAccount = ref
		case "balance":
			m.Balance = ref
		case "ignore":
			m.Ignore = append(m.Ignore, *ref)
		case "debit", "credit":
			if hasAmount {
				continue // the legacy kept them as plain columns next to the signed amount
			}
			opts.AmountMode = string(ingest.AmountDebitCredit)
			m.Amount = nil
			if roles[j] == "debit" {
				opts.DebitColumn = ref
			} else {
				opts.CreditColumn = ref
			}
		}
	}
	if m.Ignore == nil {
		m.Ignore = []ledger.ColRef{}
	}
	return m
}
