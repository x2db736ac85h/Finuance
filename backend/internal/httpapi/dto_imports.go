package httpapi

import (
	"encoding/json"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/ingest"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// Conversions between ledger types and the generated API types (accounts and imports).

func dateDTO(d *platform.Date) *openapi_types.Date {
	if d == nil {
		return nil
	}
	return &openapi_types.Date{Time: d.Time()}
}

func moneyDTO(minor *int64, currency string) *gen.Money {
	if minor == nil {
		return nil
	}
	return &gen.Money{AmountMinor: *minor, Currency: currency}
}

func accountDTO(a ledger.Account) gen.Account {
	out := gen.Account{Id: a.ID, Name: a.Name, Kind: gen.AccountKind(a.Kind), Currency: a.Currency, OpeningBalance: gen.Money{AmountMinor: a.OpeningBalanceMinor, Currency: a.Currency},
		TxCount: a.TxCount, FirstOn: dateDTO(a.FirstOn), LastOn: dateDTO(a.LastOn), IsDemo: a.IsDemo, Archived: a.ArchivedAt != nil, CreatedAt: a.CreatedAt}
	if a.NumberHint != "" {
		h := a.NumberHint
		out.NumberHint = &h
	}
	return out
}

func accountInput(in gen.AccountInput) ledger.AccountInput {
	out := ledger.AccountInput{Name: in.Name}
	if in.Currency != nil {
		out.Currency = *in.Currency
	}
	if in.NumberHint != nil {
		out.NumberHint = *in.NumberHint
	}
	if in.OpeningBalanceMinor != nil {
		out.OpeningBalanceMinor = *in.OpeningBalanceMinor
	}
	if in.Archived != nil {
		out.Archived = *in.Archived
	}
	if in.Kind != nil {
		out.Kind = string(*in.Kind)
	}
	return out
}

func rowDTO(r ledger.Row) gen.StagedRow {
	out := gen.StagedRow{RowIndex: r.RowIndex, BookedOn: dateDTO(r.BookedOn), ValueOn: dateDTO(r.ValueOn),
		Amount: moneyDTO(r.AmountMinor, r.Currency), Balance: moneyDTO(r.BalanceMinor, r.Currency),
		Payee: r.Payee, Description: r.Description, Reference: r.Reference, TxType: r.TxType, BankCategory: r.BankCategory,
		CounterpartyAccount: r.CounterpartyAccount, Duplicate: r.Duplicate, TransactionId: r.TransactionID, Raw: rawDTO(r.Raw)}
	if r.Error != "" {
		e := r.Error
		out.Error = &e
	}
	return out
}

func rawDTO(raw []ingest.RawField) []gen.RawField {
	out := make([]gen.RawField, len(raw))
	for i, f := range raw {
		out[i] = gen.RawField{Header: f.Header, Value: f.Value}
	}
	return out
}

func rowsDTO(rows []ledger.Row) []gen.StagedRow {
	out := make([]gen.StagedRow, len(rows))
	for i := range rows {
		out[i] = rowDTO(rows[i])
	}
	return out
}

// importSummaryDTO is the list form; labels maps a header signature to its b1, b2, … label.
func importSummaryDTO(i ledger.Import, labels map[[32]byte]string) gen.ImportSummary {
	out := gen.ImportSummary{Id: i.ID, AccountId: i.AccountID, Filename: i.Filename, SizeBytes: i.SizeBytes,
		Status: gen.ImportSummaryStatus(i.Status), MappingSource: gen.ImportSummaryMappingSource(i.MappingSource),
		CreatedAt: i.CreatedAt, CommittedAt: i.CommittedAt, Stats: map[string]any{}}
	_ = json.Unmarshal(i.Stats, &out.Stats)
	if l, ok := labels[i.HeaderSignature]; ok {
		out.ProfileLabel = &l
	}
	if i.Error != "" {
		e := i.Error
		out.Error = &e
	}
	return out
}

func importDetailDTO(i ledger.Import, labels map[[32]byte]string) gen.ImportDetail {
	s := importSummaryDTO(i, labels)
	out := gen.ImportDetail{Id: s.Id, AccountId: s.AccountId, Filename: s.Filename, SizeBytes: s.SizeBytes,
		Status: gen.ImportDetailStatus(s.Status), MappingSource: gen.ImportDetailMappingSource(s.MappingSource),
		CreatedAt: s.CreatedAt, CommittedAt: s.CommittedAt, Stats: s.Stats, ProfileLabel: s.ProfileLabel, Error: s.Error,
		Encoding: i.Encoding, Delimiter: i.Delimiter, Headers: i.Headers, Options: ledger.DefaultOptions()}
	out.ColumnTypes = make([]gen.ImportDetailColumnTypes, len(i.ColumnTypes))
	for k, t := range i.ColumnTypes {
		out.ColumnTypes[k] = gen.ImportDetailColumnTypes(t)
	}
	_ = json.Unmarshal(i.Mapping, &out.Mapping)
	_ = json.Unmarshal(i.Options, &out.Options)
	if out.Mapping.Ignore == nil {
		out.Mapping.Ignore = []ledger.ColRef{}
	}
	return out
}

func previewDTO(p ledger.Preview, labels map[[32]byte]string) gen.ImportPreview {
	out := gen.ImportPreview{Import: importDetailDTO(p.Import, labels), SampleRows: rowsDTO(p.SampleRows), TailRows: rowsDTO(p.TailRows),
		Missing: append([]string{}, p.Missing...), Warnings: []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}{}}
	// The preview's own documents are authoritative (they may differ from what JSON round-trips).
	out.Import.Mapping, out.Import.Options = p.Mapping, p.Options
	if out.Import.Mapping.Ignore == nil {
		out.Import.Mapping.Ignore = []ledger.ColRef{}
	}
	out.Detected = p.Detected
	if out.Detected.Ignore == nil {
		out.Detected.Ignore = []ledger.ColRef{}
	}
	s := p.Stats
	out.Stats = gen.ImportStats{Rows: s.Rows, Errors: s.Errors, DuplicatesEstimate: s.DuplicatesEstimate, NewEstimate: s.NewEstimate,
		DateFrom: dateDTO(s.DateFrom), DateTo: dateDTO(s.DateTo), HeaderAt: s.HeaderAt, Dropped: s.Dropped}
	out.SuggestedAccount.Id = p.Suggested.ID
	if p.Suggested.CreateName != "" {
		n := p.Suggested.CreateName
		out.SuggestedAccount.CreateName = &n
	}
	for _, w := range p.Warnings {
		out.Warnings = append(out.Warnings, struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}{w.Code, w.Message})
	}
	return out
}

func linesDTO(l *ingest.LinesResult) gen.ImportLines {
	out := gen.ImportLines{Encoding: string(l.Encoding), Delimiter: ledger.DelimiterName(l.Delimiter), Total: l.Total}
	for _, ln := range l.Lines {
		fields := ln.Fields
		if fields == nil {
			fields = []string{}
		}
		out.Lines = append(out.Lines, struct {
			Fields []string                   `json:"fields"`
			Record int                        `json:"record"`
			Status gen.ImportLinesLinesStatus `json:"status"`
		}{fields, ln.Record, gen.ImportLinesLinesStatus(ln.Status)})
	}
	if out.Lines == nil {
		out.Lines = []struct {
			Fields []string                   `json:"fields"`
			Record int                        `json:"record"`
			Status gen.ImportLinesLinesStatus `json:"status"`
		}{}
	}
	return out
}
