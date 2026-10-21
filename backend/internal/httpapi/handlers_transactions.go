package httpapi

import (
	"bufio"
	"context"
	"io"
	"mime"
	"regexp"
	"time"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/observability"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
)

// Transactions and categories (PLAN tasks 7.5–7.9, 7.12).

// QueryTransactions returns one window of the filtered, sorted rows with the totals.
func (a *api) QueryTransactions(ctx context.Context, req gen.QueryTransactionsRequestObject) (gen.QueryTransactionsResponseObject, error) {
	b := req.Body
	r := query.PageRequest{Context: contextIn(b.Context), Sort: sortIn(b.Sort), Offset: deref(b.Offset), Limit: b.Limit, LocateID: b.LocateId,
		SkipTotals: b.IncludeTotals != nil && !*b.IncludeTotals}
	if b.StatColumn != nil {
		r.StatColumn = string(*b.StatColumn)
	}
	p, err := a.query.Page(ctx, identity(ctx).User.ID, r)
	if err != nil {
		return nil, err
	}
	return gen.QueryTransactions200JSONResponse(pageDTO(&p)), nil
}

// DistinctValues lists a column's values with counts for the value checklist.
func (a *api) DistinctValues(ctx context.Context, req gen.DistinctValuesRequestObject) (gen.DistinctValuesResponseObject, error) {
	b := req.Body
	limit := query.DefaultDistinct
	if b.Limit != nil {
		limit = *b.Limit
	}
	vals, err := a.query.Distinct(ctx, identity(ctx).User.ID, contextIn(b.Context), string(b.Column), deref(b.Search), limit)
	if err != nil {
		return nil, err
	}
	out := make(gen.DistinctValues200JSONResponse, len(vals))
	for i, v := range vals {
		out[i] = gen.DistinctValue{Value: v.Value, Count: v.Count}
	}
	return out, nil
}

// TransactionRange returns ids and amounts of a range of the ordered result.
func (a *api) TransactionRange(ctx context.Context, req gen.TransactionRangeRequestObject) (gen.TransactionRangeResponseObject, error) {
	b := req.Body
	ids, err := a.query.IDs(ctx, identity(ctx).User.ID, contextIn(b.Context), sortIn(b.Sort), deref(b.Offset), b.Limit)
	if err != nil {
		return nil, err
	}
	out := make(gen.TransactionRange200JSONResponse, len(ids))
	for i, x := range ids {
		out[i] = gen.IdAmount{Id: x.ID, AmountMinor: x.AmountMinor}
	}
	return out, nil
}

// GetTransaction returns one transaction with its raw fields and source imports.
func (a *api) GetTransaction(ctx context.Context, req gen.GetTransactionRequestObject) (gen.GetTransactionResponseObject, error) {
	d, err := a.transactionDetail(ctx, identity(ctx).User.ID, req.Id)
	if err != nil {
		return nil, err
	}
	return gen.GetTransaction200JSONResponse(d), nil
}

// UpdateTransaction sets the category or the note of one transaction.
func (a *api) UpdateTransaction(ctx context.Context, req gen.UpdateTransactionRequestObject) (gen.UpdateTransactionResponseObject, error) {
	u, b := identity(ctx).User.ID, req.Body
	if _, err := a.ledger.UpdateTransaction(ctx, u, req.Id, ledger.TransactionPatch{CategoryID: b.CategoryId, CategoryName: b.CategoryName,
		ResetCategory: deref(b.ResetCategory), Note: b.Note}); err != nil {
		return nil, err
	}
	d, err := a.transactionDetail(ctx, u, req.Id)
	if err != nil {
		return nil, err
	}
	return gen.UpdateTransaction200JSONResponse(d), nil
}

func (a *api) transactionDetail(ctx context.Context, userID, id uuid.UUID) (gen.TransactionDetail, error) {
	t, err := a.ledger.GetTransaction(ctx, userID, id)
	if err != nil {
		return gen.TransactionDetail{}, err
	}
	acc, err := a.ledger.GetAccount(ctx, userID, t.AccountID)
	if err != nil {
		return gen.TransactionDetail{}, err
	}
	src, err := a.ledger.TransactionProvenance(ctx, userID, id)
	if err != nil {
		return gen.TransactionDetail{}, err
	}
	return txDetailDTO(&t, acc.Name, src), nil
}

// ListCategories returns the categories with counts and colour slots over the given accounts.
func (a *api) ListCategories(ctx context.Context, req gen.ListCategoriesRequestObject) (gen.ListCategoriesResponseObject, error) {
	cats, err := a.query.Categories(ctx, identity(ctx).User.ID, deref(req.Params.AccountIds))
	if err != nil {
		return nil, err
	}
	out := make(gen.ListCategories200JSONResponse, len(cats))
	for i, c := range cats {
		out[i] = gen.Category{Id: c.ID, Name: c.Name, TxCount: c.TxCount, ColorSlot: c.ColorSlot}
	}
	return out, nil
}

var delimiters = map[gen.ExportRequestDelimiter]string{gen.DelimiterSemicolon: ";", gen.DelimiterComma: ",", gen.DelimiterTab: "\t", gen.DelimiterPipe: "|"}

var unsafeFileChars = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

// ExportTransactions streams the rows as CSV (ADR 0014). Everything that can fail with a problem
// is checked first; once the body has started, an error can only cut the stream short.
func (a *api) ExportTransactions(ctx context.Context, req gen.ExportTransactionsRequestObject) (gen.ExportTransactionsResponseObject, error) {
	u, b := identity(ctx).User.ID, req.Body
	c, s := contextIn(b.Context), sortIn(b.Sort)
	if err := query.Validate(u, c, s); err != nil {
		return nil, err
	}
	cols := make([]string, len(b.Columns))
	header := make([]string, len(b.Columns))
	for i, name := range b.Columns {
		col, _ := query.Lookup(string(name)) // the OpenAPI enum is the registry
		cols[i], header[i] = col.Name, col.Label
	}
	delim := ";"
	if b.Delimiter != nil {
		delim = delimiters[*b.Delimiter]
	}
	view := unsafeFileChars.ReplaceAllString(deref(b.View), "_")
	if view == "" {
		view = "transactions"
	}
	name := "finuance-" + view + "-" + time.Now().UTC().Format("2006-01-02") + ".csv"
	pr, pw := io.Pipe()
	go func() {
		w := bufio.NewWriterSize(pw, 64<<10)
		_, _ = w.WriteString("\ufeff" + query.CSVLine(header, delim))
		err := a.query.Export(ctx, u, c, s, cols, func(row []string) error {
			_, err := w.WriteString("\r\n" + query.CSVLine(row, delim))
			return err
		})
		if err == nil {
			err = w.Flush()
		}
		if err != nil {
			observability.Logger(ctx).WarnContext(ctx, "export stopped", "error", err)
		}
		_ = pw.CloseWithError(err)
	}()
	cd := mime.FormatMediaType("attachment", map[string]string{"filename": name})
	return gen.ExportTransactions200TextcsvResponse{Body: pr, Headers: gen.ExportTransactions200ResponseHeaders{ContentDisposition: &cd}}, nil
}
