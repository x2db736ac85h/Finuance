package httpapi

import (
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
)

// Conversions between the query/ledger types and the generated API types (transactions).

func dateIn(d *openapi_types.Date) *platform.Date {
	if d == nil {
		return nil
	}
	v := platform.DateOf(d.Time, time.UTC)
	return &v
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

func contextIn(c gen.QueryContext) query.Context {
	out := query.Context{AccountIDs: deref(c.AccountIds), Search: deref(c.Search), IncludeTransfers: deref(c.IncludeTransfers)}
	for _, f := range deref(c.Filters) {
		out.Filters = append(out.Filters, query.Filter{Column: string(f.Column), Cond: deref(f.Cond), Excluded: deref(f.Excluded)})
	}
	if p := c.Period; p != nil {
		out.Period = &query.Period{Preset: string(deref(p.Preset)), Month: deref(p.Month), Year: deref(p.Year), From: dateIn(p.From), To: dateIn(p.To)}
	}
	if f := c.ToolFilter; f != nil {
		out.ToolFilter = &query.ToolFilter{Query: deref(f.Query), Payee: deref(f.Payee), Category: deref(f.Category), Group: deref(f.Group),
			DateFrom: dateIn(f.DateFrom), DateTo: dateIn(f.DateTo), Type: string(deref(f.Type)), MinAmount: f.MinAmount, MaxAmount: f.MaxAmount,
			Label: deref(f.Label)}
	}
	return out
}

func sortIn(s *gen.Sort) *query.Sort {
	if s == nil {
		return nil
	}
	return &query.Sort{Column: string(s.Column), Desc: s.Dir != nil && *s.Dir == gen.Desc}
}

func namedRef(id *openapi_types.UUID, name *string) *gen.NamedRef {
	if id == nil || name == nil {
		return nil
	}
	return &gen.NamedRef{Id: *id, Name: *name}
}

func txRowDTO(r *query.Row) gen.TransactionRow {
	return gen.TransactionRow{Id: r.ID, AccountId: r.AccountID, AccountName: r.AccountName, BookedOn: *dateDTO(&r.BookedOn),
		ValueOn: dateDTO(r.ValueOn), Amount: gen.Money{AmountMinor: r.AmountMinor, Currency: r.Currency},
		BalanceAfter: moneyDTO(r.BalanceAfterMinor, r.Currency), Payee: r.Payee, Description: r.Description,
		Category: namedRef(r.CategoryID, r.CategoryName), CategorySource: gen.TransactionRowCategorySource(r.CategorySource),
		BankCategory: r.BankCategory, TxType: r.TxType, Reference: r.Reference, CounterpartyAccount: r.CounterpartyAccount,
		Note: r.Note, IsTransfer: r.IsTransfer, Group: namedRef(r.GroupID, r.GroupName)}
}

func pageDTO(p *query.Page) gen.TransactionPage {
	out := gen.TransactionPage{Rows: make([]gen.TransactionRow, len(p.Rows)), Total: p.Total, Offset: p.Offset,
		DuplicatesSkipped: p.DuplicatesSkipped, LocatedOffset: p.LocatedOffset,
		Stats: gen.ColumnStats{Column: gen.ColumnStatsColumn(p.Stats.Column), Sum: p.Stats.Sum, Avg: p.Stats.Avg, Min: p.Stats.Min, Max: p.Stats.Max}}
	for i := range p.Rows {
		out.Rows[i] = txRowDTO(&p.Rows[i])
	}
	if r := p.Period; r != nil {
		out.Period = &gen.ResolvedPeriod{Kind: gen.ResolvedPeriodKind(r.Kind), From: *dateDTO(&r.From), To: *dateDTO(&r.To), Label: r.Label}
	}
	if p.DataMin != nil && p.DataMax != nil {
		out.DataRange = &gen.DateRange{Min: *dateDTO(p.DataMin), Max: *dateDTO(p.DataMax)}
	}
	return out
}

func txDetailDTO(t *ledger.Transaction, accountName string, sources []ledger.Provenance) gen.TransactionDetail {
	out := gen.TransactionDetail{Id: t.ID, AccountId: t.AccountID, AccountName: accountName, BookedOn: *dateDTO(&t.BookedOn),
		ValueOn: dateDTO(t.ValueOn), Amount: gen.Money{AmountMinor: t.AmountMinor, Currency: t.Currency},
		BalanceAfter: moneyDTO(t.BalanceAfterMinor, t.Currency), Payee: t.Payee, Description: t.Description,
		CategorySource: gen.TransactionDetailCategorySource(t.CategorySource), BankCategory: t.BankCategory, TxType: t.TxType,
		Reference: t.Reference, CounterpartyAccount: t.CounterpartyAccount, Note: t.Note, Origin: gen.TransactionDetailOrigin(t.Origin),
		Raw: rawDTO(t.Raw), Sources: make([]gen.TransactionSource, len(sources)), CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt}
	if t.CategoryID != nil {
		out.Category = &gen.NamedRef{Id: *t.CategoryID, Name: t.CategoryName}
	}
	if t.GroupID != nil {
		out.Group = &gen.NamedRef{Id: *t.GroupID, Name: t.GroupName}
	}
	for i, s := range sources {
		out.Sources[i] = gen.TransactionSource{ImportId: s.ImportID, Filename: s.Filename, CommittedAt: s.CommittedAt, RowIndex: s.RowIndex, Raw: rawDTO(s.Raw)}
	}
	return out
}
