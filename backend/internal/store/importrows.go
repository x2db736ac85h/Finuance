package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// ImportRowBatch is the number of staged rows per INSERT statement. 5,000 rows × 25 columns stays
// well below PostgreSQL's 1 GB parameter limit and keeps each statement short (PLAN task 4.4).
const ImportRowBatch = 5000

// ImportRow is one staged row as stored in import_rows. Errored rows carry ParseError and leave the
// canonical fields empty: a nil Fingerprint, Occurrence and AmountMinor, and zero dates, are stored
// as NULL.
type ImportRow struct {
	RowIndex            int32
	Fingerprint         []byte
	Occurrence          *int32
	SplitScope          uuid.UUID
	BookedOn            platform.Date
	ValueOn             platform.Date
	AmountMinor         *int64
	Currency            string
	Payee               string
	PayeeN              string
	PayeeGroup          string
	Description         string
	DescriptionN        string
	Reference           string
	ReferenceN          string
	TxType              string
	TxTypeN             string
	BankCategory        string
	BankCategoryN       string
	CounterpartyAccount string
	CounterpartyN       string
	BalanceAfterMinor   *int64
	Raw                 []byte // JSON array of {"h": header, "v": value}, in column order
	SearchText          string
	ParseError          *string
}

// insertImportRows zips one array per column with unnest. COPY FROM is not an option: PostgreSQL
// refuses COPY FROM into a table with row-level security enabled (ADR 0007), and import_rows has it.
const insertImportRows = `
INSERT INTO import_rows (import_id, user_id, row_index, fingerprint, occurrence, split_scope, booked_on, value_on,
                         amount_minor, currency, payee, payee_n, payee_group, description, description_n,
                         reference, reference_n, tx_type, tx_type_n, bank_category, bank_category_n,
                         counterparty_account, counterparty_n, balance_after_minor, raw, search_text, parse_error)
SELECT $1, $2, u.row_index, u.fingerprint, u.occurrence, u.split_scope, u.booked_on, u.value_on,
       u.amount_minor, nullif(u.currency, ''), u.payee, u.payee_n, u.payee_group, u.description, u.description_n,
       u.reference, u.reference_n, u.tx_type, u.tx_type_n, u.bank_category, u.bank_category_n,
       u.counterparty_account, u.counterparty_n, u.balance_after_minor, u.raw::jsonb, u.search_text, u.parse_error
FROM unnest($3::int[], $4::bytea[], $5::int[], $6::uuid[], $7::date[], $8::date[],
            $9::bigint[], $10::text[], $11::text[], $12::text[], $13::text[], $14::text[], $15::text[],
            $16::text[], $17::text[], $18::text[], $19::text[], $20::text[], $21::text[],
            $22::text[], $23::text[], $24::bigint[], $25::text[], $26::text[], $27::text[])
  AS u(row_index, fingerprint, occurrence, split_scope, booked_on, value_on,
       amount_minor, currency, payee, payee_n, payee_group, description, description_n,
       reference, reference_n, tx_type, tx_type_n, bank_category, bank_category_n,
       counterparty_account, counterparty_n, balance_after_minor, raw, search_text, parse_error)`

// InsertImportRows stores staged rows of one import in batches of ImportRowBatch. It must run inside
// WithUser (tx is the callback's transaction): the RLS policy checks user_id on every row. It
// returns the number of rows inserted.
func InsertImportRows(ctx context.Context, tx pgx.Tx, importID, userID uuid.UUID, rows []ImportRow) (int64, error) {
	var total int64
	for start := 0; start < len(rows); start += ImportRowBatch {
		batch := rows[start:min(start+ImportRowBatch, len(rows))]
		tag, err := tx.Exec(ctx, insertImportRows, columns(importID, userID, batch)...)
		if err != nil {
			return total, fmt.Errorf("insert import rows %d–%d: %w", start, start+len(batch)-1, err)
		}
		total += tag.RowsAffected()
	}
	return total, nil
}

// columns turns rows into the 27 statement arguments: the two ids, then one slice per column.
func columns(importID, userID uuid.UUID, rows []ImportRow) []any {
	n := len(rows)
	var (
		rowIndex   = make([]int32, n)
		fp         = make([][]byte, n)
		occ        = make([]*int32, n)
		scope      = make([][16]byte, n) // [16]byte encodes directly; uuid.UUID would go through its string form
		booked     = make([]platform.Date, n)
		valueOn    = make([]platform.Date, n)
		amount     = make([]*int64, n)
		balance    = make([]*int64, n)
		raw        = make([]string, n)
		parseError = make([]*string, n)
		text       [14][]string // currency … counterparty_n, in statement order
	)
	for k := range text {
		text[k] = make([]string, n)
	}
	for i := range rows {
		r := &rows[i]
		rowIndex[i], fp[i], occ[i], scope[i] = r.RowIndex, r.Fingerprint, r.Occurrence, r.SplitScope
		booked[i], valueOn[i], amount[i], balance[i] = r.BookedOn, r.ValueOn, r.AmountMinor, r.BalanceAfterMinor
		raw[i], parseError[i] = string(r.Raw), r.ParseError
		if len(r.Raw) == 0 {
			raw[i] = "[]"
		}
		for k, v := range [...]string{r.Currency, r.Payee, r.PayeeN, r.PayeeGroup, r.Description, r.DescriptionN,
			r.Reference, r.ReferenceN, r.TxType, r.TxTypeN, r.BankCategory, r.BankCategoryN, r.CounterpartyAccount, r.CounterpartyN} {
			text[k][i] = v
		}
	}
	search := make([]string, n)
	for i := range rows {
		search[i] = rows[i].SearchText
	}
	return []any{importID, userID, rowIndex, fp, occ, scope, booked, valueOn, amount,
		text[0], text[1], text[2], text[3], text[4], text[5], text[6], text[7], text[8], text[9], text[10],
		text[11], text[12], text[13], balance, raw, search, parseError}
}
