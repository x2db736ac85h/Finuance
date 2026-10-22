package ledger

import (
	"context"
	"encoding/hex"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/ingest"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// The duplicates window (PLAN §7.14, legacy §32.3): every (account, fingerprint) whose staged rows
// come from two or more imports and where something was merged or the user decided, with every
// version side by side. The decisions themselves are SetOverride / ClearOverride.

// MaxDuplicatePage is the most groups one page holds (legacy: 60 at a time).
const MaxDuplicatePage = 60

// DuplicateFields are the canonical values of one staged row.
type DuplicateFields struct {
	BookedOn            *platform.Date
	ValueOn             *platform.Date
	AmountMinor         *int64
	BalanceMinor        *int64
	Currency            string
	Payee               string
	Description         string
	Reference           string
	TxType              string
	BankCategory        string
	CounterpartyAccount string
}

// DuplicateRow is one version of the transaction in one import.
type DuplicateRow struct {
	Occurrence int
	Fields     DuplicateFields
	Raw        []ingest.RawField
	// InData: this row is the source of the transaction the data shows.
	InData bool
}

// DuplicateVersion is one import's rows of the group.
type DuplicateVersion struct {
	ImportID    uuid.UUID
	Filename    string
	CommittedAt *time.Time
	Rows        []DuplicateRow
}

// DuplicateOverride is the user's decision for a group, if any.
type DuplicateOverride struct {
	Mode     OverrideMode
	ImportID *uuid.UUID
}

// DuplicateGroup is one de-dupe key with all its versions, earliest-committed import first.
type DuplicateGroup struct {
	AccountID   uuid.UUID
	Fingerprint []byte
	Override    *DuplicateOverride
	// Key holds the values of the first version (the key fields of the window's header).
	Key DuplicateFields
	// Differing names the fields whose values differ between the versions, in API names
	// (bookedOn, valueOn, amount, balanceAfter, payee, description, reference, txType,
	// bankCategory, counterpartyAccount).
	Differing []string
	Versions  []DuplicateVersion
}

// DuplicatePage is one page of groups and the number of groups that match.
type DuplicatePage struct {
	Total  int
	Groups []DuplicateGroup
}

// Duplicates lists the duplicate groups of accountIDs (every account when empty), newest first.
// search keeps the groups where a row contains every word (accent- and case-insensitive).
func (l *Ledger) Duplicates(ctx context.Context, userID uuid.UUID, accountIDs []uuid.UUID, search string, offset, limit int) (DuplicatePage, error) {
	ve := &platform.ValidationError{}
	if limit < 1 || limit > MaxDuplicatePage {
		ve.Add("limit", "must be 1–60")
	}
	if offset < 0 || offset > 1<<30 {
		ve.Add("offset", "must not be negative")
	}
	if err := ve.Err(); err != nil {
		return DuplicatePage{}, err
	}
	tokens := ingest.FieldsJS(ingest.Norm(search))
	if tokens == nil {
		tokens = []string{}
	}
	var out DuplicatePage
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		ids := accountIDs
		if len(ids) == 0 {
			accs, err := q.ListAccounts(ctx, userID)
			if err != nil {
				return err
			}
			for i := range accs {
				ids = append(ids, accs[i].ID)
			}
		}
		keys, err := q.DuplicateGroupKeys(ctx, db.DuplicateGroupKeysParams{UserID: userID, AccountIds: ids, Tokens: tokens,
			RowOffset: int32(offset), RowLimit: int32(limit)}) //nolint:gosec // both checked above
		if err != nil || len(keys) == 0 {
			return err
		}
		out.Total = int(keys[0].Total)
		out.Groups, err = loadGroups(ctx, q, userID, keys)
		return err
	})
	return out, dbErr("duplicates", err)
}

// DuplicateGroupOf returns one group (after a decision), or ErrNotFound when it has no versions.
func (l *Ledger) DuplicateGroupOf(ctx context.Context, userID, accountID uuid.UUID, fingerprint []byte) (DuplicateGroup, error) {
	var out []DuplicateGroup
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) (err error) {
		out, err = loadGroups(ctx, q, userID, []db.DuplicateGroupKeysRow{{AccountID: accountID, Fingerprint: fingerprint}})
		return err
	})
	if err == nil && len(out) == 0 {
		err = ErrNotFound
	}
	if err != nil {
		return DuplicateGroup{}, dbErr("duplicate group", err)
	}
	return out[0], nil
}

func pairKey(accountID uuid.UUID, fp []byte) string {
	return accountID.String() + ":" + hex.EncodeToString(fp)
}

// loadGroups builds the groups of keys, in the order of keys.
func loadGroups(ctx context.Context, q *db.Queries, userID uuid.UUID, keys []db.DuplicateGroupKeysRow) ([]DuplicateGroup, error) {
	fps := make([][]byte, len(keys))
	accs := make([]uuid.UUID, len(keys))
	pairs := make([]string, len(keys))
	byKey := make(map[string]*DuplicateGroup, len(keys))
	groups := make([]DuplicateGroup, len(keys))
	for i, k := range keys {
		fps[i], accs[i], pairs[i] = k.Fingerprint, k.AccountID, pairKey(k.AccountID, k.Fingerprint)
		groups[i] = DuplicateGroup{AccountID: k.AccountID, Fingerprint: k.Fingerprint}
		byKey[pairs[i]] = &groups[i]
	}
	rows, err := q.DuplicateGroupRows(ctx, db.DuplicateGroupRowsParams{UserID: userID, Fingerprints: fps, AccountIds: accs, PairKeys: pairs})
	if err != nil {
		return nil, err
	}
	for i := range rows {
		r := &rows[i]
		g := byKey[pairKey(r.AccountID, r.Fingerprint)]
		if g == nil {
			continue
		}
		raw, err := decodeRaw(r.Raw)
		if err != nil {
			return nil, err
		}
		row := DuplicateRow{Fields: DuplicateFields{BookedOn: r.BookedOn, ValueOn: r.ValueOn, AmountMinor: r.AmountMinor,
			BalanceMinor: r.BalanceAfterMinor, Currency: str(r.Currency), Payee: str(r.Payee), Description: str(r.Description),
			Reference: str(r.Reference), TxType: str(r.TxType), BankCategory: str(r.BankCategory),
			CounterpartyAccount: str(r.CounterpartyAccount)}, Raw: raw, InData: r.InData}
		if r.Occurrence != nil {
			row.Occurrence = int(*r.Occurrence)
		}
		n := len(g.Versions)
		if n == 0 || g.Versions[n-1].ImportID != r.ImportID {
			g.Versions = append(g.Versions, DuplicateVersion{ImportID: r.ImportID, Filename: r.Filename, CommittedAt: r.CommittedAt})
			n++
		}
		g.Versions[n-1].Rows = append(g.Versions[n-1].Rows, row)
	}
	overrides, err := q.ListDedupeOverridesFor(ctx, db.ListDedupeOverridesForParams{UserID: userID, Fingerprints: fps, PairKeys: pairs})
	if err != nil {
		return nil, err
	}
	for i := range overrides {
		o := &overrides[i]
		if g := byKey[pairKey(o.AccountID, o.Fingerprint)]; g != nil {
			g.Override = &DuplicateOverride{Mode: OverrideMode(o.Mode), ImportID: o.PreferredImportID}
		}
	}
	out := groups[:0]
	for i := range groups {
		g := &groups[i]
		if len(g.Versions) == 0 {
			continue
		}
		g.Key = g.Versions[0].Rows[0].Fields
		g.Differing = differing(g.Versions)
		out = append(out, *g)
	}
	return out, nil
}

func str(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// differing returns the fields whose values are not the same in every row of the versions.
func differing(versions []DuplicateVersion) []string {
	type field struct {
		name string
		val  func(f *DuplicateFields) string
	}
	date := func(d *platform.Date) string {
		if d == nil {
			return ""
		}
		return d.String()
	}
	minor := func(m *int64) string {
		if m == nil {
			return ""
		}
		return strconv.FormatInt(*m, 10)
	}
	fields := []field{
		{"bookedOn", func(f *DuplicateFields) string { return date(f.BookedOn) }},
		{"valueOn", func(f *DuplicateFields) string { return date(f.ValueOn) }},
		{"amount", func(f *DuplicateFields) string { return minor(f.AmountMinor) + " " + f.Currency }},
		{"balanceAfter", func(f *DuplicateFields) string { return minor(f.BalanceMinor) }},
		{"payee", func(f *DuplicateFields) string { return f.Payee }},
		{"description", func(f *DuplicateFields) string { return f.Description }},
		{"reference", func(f *DuplicateFields) string { return f.Reference }},
		{"txType", func(f *DuplicateFields) string { return f.TxType }},
		{"bankCategory", func(f *DuplicateFields) string { return f.BankCategory }},
		{"counterpartyAccount", func(f *DuplicateFields) string { return f.CounterpartyAccount }},
	}
	var out []string
	for _, fd := range fields {
		first, same := "", true
		seen := false
		for vi := range versions {
			for ri := range versions[vi].Rows {
				v := fd.val(&versions[vi].Rows[ri].Fields)
				if !seen {
					first, seen = v, true
				} else if v != first {
					same = false
				}
			}
		}
		if !same {
			out = append(out, fd.name)
		}
	}
	return out
}
