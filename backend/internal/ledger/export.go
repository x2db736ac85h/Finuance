package ledger

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// The personal data export (GET /me/export, PLAN task 7.10): every entity the user owns as plain
// JSON documents. The HTTP layer adds preferences, transactions.csv and the original files. Each
// later phase adds its entity here (assistant chats: Phase 13).

// ExportAccount is an account in the export.
type ExportAccount struct {
	ID                  uuid.UUID  `json:"id"`
	Name                string     `json:"name"`
	Currency            string     `json:"currency"`
	NumberHint          string     `json:"numberHint,omitempty"`
	OpeningBalanceMinor int64      `json:"openingBalanceMinor"`
	IsDemo              bool       `json:"isDemo"`
	ArchivedAt          *time.Time `json:"archivedAt,omitempty"`
	CreatedAt           time.Time  `json:"createdAt"`
}

// ExportNamed is a category or group.
type ExportNamed struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Color       *int      `json:"color,omitempty"`
	BudgetMinor *int64    `json:"budgetMinor,omitempty"` // groups (Phase 15)
}

// ExportRule is a categorisation rule (Phase 15): what it matches, in the table's filter
// language, and what it does.
type ExportRule struct {
	ID            uuid.UUID       `json:"id"`
	Name          string          `json:"name"`
	Enabled       bool            `json:"enabled"`
	Position      int             `json:"position"`
	Match         json.RawMessage `json:"match"`
	SetCategoryID *uuid.UUID      `json:"setCategoryId,omitempty"`
	SetCategory   *string         `json:"setCategory,omitempty"`
	MarkTransfer  bool            `json:"markTransfer"`
	UpdatedAt     time.Time       `json:"updatedAt"`
}

// ExportAlias is where a bank category name now goes after a rename, merge or delete (Phase 15);
// no category means uncategorised.
type ExportAlias struct {
	BankCategory string     `json:"bankCategory"` // normalised (lower case, no accents)
	CategoryID   *uuid.UUID `json:"categoryId,omitempty"`
	CategoryName *string    `json:"categoryName,omitempty"`
}

// ExportBudget is a category budget override.
type ExportBudget struct {
	CategoryID   uuid.UUID `json:"categoryId"`
	CategoryName string    `json:"categoryName"`
	AmountMinor  int64     `json:"amountMinor"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// ExportAnnotation is a user edit keyed by the dedupe identity (fingerprint in hex).
type ExportAnnotation struct {
	AccountID        uuid.UUID  `json:"accountId"`
	Fingerprint      string     `json:"fingerprint"`
	Occurrence       int        `json:"occurrence"`
	SplitScope       uuid.UUID  `json:"splitScope"`
	GroupID          *uuid.UUID `json:"groupId,omitempty"`
	CategoryID       *uuid.UUID `json:"categoryId,omitempty"`
	Note             string     `json:"note,omitempty"`
	TransferOverride *bool      `json:"transferOverride,omitempty"`
	UpdatedAt        time.Time  `json:"updatedAt"`
}

// ExportOverride is a duplicate-review decision.
type ExportOverride struct {
	AccountID         uuid.UUID  `json:"accountId"`
	Fingerprint       string     `json:"fingerprint"`
	Mode              string     `json:"mode"`
	PreferredImportID *uuid.UUID `json:"preferredImportId,omitempty"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}

// ExportImport is an import's metadata (its file is exported separately).
type ExportImport struct {
	ID            uuid.UUID       `json:"id"`
	AccountID     *uuid.UUID      `json:"accountId,omitempty"`
	Filename      string          `json:"filename"`
	SizeBytes     int             `json:"sizeBytes"`
	Status        ImportStatus    `json:"status"`
	Encoding      string          `json:"encoding"`
	Delimiter     string          `json:"delimiter"`
	Headers       []string        `json:"headers"`
	MappingSource string          `json:"mappingSource"`
	Mapping       json.RawMessage `json:"mapping"`
	Options       json.RawMessage `json:"options"`
	Stats         json.RawMessage `json:"stats"`
	CreatedAt     time.Time       `json:"createdAt"`
	CommittedAt   *time.Time      `json:"committedAt,omitempty"`
}

// ExportInvestments is the manual investment data.
type ExportInvestments struct {
	PPK struct {
		EmployeeMinor int64 `json:"employeeMinor"`
		EmployerMinor int64 `json:"employerMinor"`
		StateMinor    int64 `json:"stateMinor"`
		ValueMinor    int64 `json:"valueMinor"`
	} `json:"ppk"`
	SwrBp        int `json:"swrBp"`
	RealReturnBp int `json:"realReturnBp"`
	Positions    []struct {
		ID                uuid.UUID `json:"id"`
		Name              string    `json:"name"`
		Type              string    `json:"type"`
		InvestedMinor     int64     `json:"investedMinor"`
		ValueMinor        int64     `json:"valueMinor"`
		AnnualIncomeMinor int64     `json:"annualIncomeMinor"`
	} `json:"positions"`
}

// Export returns the user's entities by export file name (accounts.json, …).
func (l *Ledger) Export(ctx context.Context, userID uuid.UUID) (map[string]any, error) {
	out := map[string]any{}
	err := l.st.WithUserReadOnly(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		accs, err := q.ListAccounts(ctx, userID)
		if err != nil {
			return err
		}
		accounts := make([]ExportAccount, len(accs))
		for i := range accs {
			a := &accs[i]
			accounts[i] = ExportAccount{ID: a.ID, Name: a.Name, Currency: a.Currency, NumberHint: deref(a.NumberHint),
				OpeningBalanceMinor: a.OpeningBalanceMinor, IsDemo: a.IsDemo, ArchivedAt: a.ArchivedAt, CreatedAt: a.CreatedAt}
		}
		out["accounts.json"] = accounts

		cats, err := q.ListCategories(ctx, userID)
		if err != nil {
			return err
		}
		categories := make([]ExportNamed, len(cats))
		for i, c := range cats {
			categories[i] = ExportNamed{ID: c.ID, Name: c.Name}
		}
		out["categories.json"] = categories

		grps, err := q.ListGroups(ctx, userID)
		if err != nil {
			return err
		}
		groups := make([]ExportNamed, len(grps))
		for i, g := range grps {
			c := int(g.Color)
			groups[i] = ExportNamed{ID: g.ID, Name: g.Name, Color: &c, BudgetMinor: g.BudgetMinor}
		}
		out["groups.json"] = groups

		rs, err := q.ListRules(ctx, userID)
		if err != nil {
			return err
		}
		rules := make([]ExportRule, len(rs))
		for i := range rs {
			r := &rs[i]
			rules[i] = ExportRule{ID: r.ID, Name: r.Name, Enabled: r.Enabled, Position: int(r.Position), Match: r.Match,
				SetCategoryID: r.SetCategoryID, SetCategory: r.CategoryName, MarkTransfer: r.MarkTransfer, UpdatedAt: r.UpdatedAt}
		}
		out["rules.json"] = rules

		as, err := q.ListCategoryAliases(ctx, userID)
		if err != nil {
			return err
		}
		aliases := make([]ExportAlias, len(as))
		for i, a := range as {
			aliases[i] = ExportAlias{BankCategory: a.NameN, CategoryID: a.CategoryID, CategoryName: a.CategoryName}
		}
		out["category-aliases.json"] = aliases

		bs, err := q.ListBudgets(ctx, userID)
		if err != nil {
			return err
		}
		budgets := make([]ExportBudget, len(bs))
		for i, b := range bs {
			budgets[i] = ExportBudget{CategoryID: b.CategoryID, CategoryName: b.Name, AmountMinor: b.AmountMinor, UpdatedAt: b.UpdatedAt}
		}
		out["budgets.json"] = budgets

		ans, err := q.ExportAnnotations(ctx, userID)
		if err != nil {
			return err
		}
		annotations := make([]ExportAnnotation, len(ans))
		for i := range ans {
			a := &ans[i]
			annotations[i] = ExportAnnotation{AccountID: a.AccountID, Fingerprint: hex.EncodeToString(a.Fingerprint), Occurrence: int(a.Occurrence),
				SplitScope: a.SplitScope, GroupID: a.GroupID, CategoryID: a.CategoryID, Note: a.Note, TransferOverride: a.TransferOverride, UpdatedAt: a.UpdatedAt}
		}
		out["annotations.json"] = annotations

		ovs, err := q.ExportDedupeOverrides(ctx, userID)
		if err != nil {
			return err
		}
		overrides := make([]ExportOverride, len(ovs))
		for i, o := range ovs {
			overrides[i] = ExportOverride{AccountID: o.AccountID, Fingerprint: hex.EncodeToString(o.Fingerprint), Mode: o.Mode,
				PreferredImportID: o.PreferredImportID, UpdatedAt: o.UpdatedAt}
		}
		out["dedupe-overrides.json"] = overrides

		imps, err := q.ListImports(ctx, db.ListImportsParams{UserID: userID, MaxRows: 1 << 30})
		if err != nil {
			return err
		}
		imports := make([]ExportImport, 0, len(imps))
		for k := range imps {
			i, err := importOf(imps[k])
			if err != nil {
				return err
			}
			imports = append(imports, ExportImport{ID: i.ID, AccountID: i.AccountID, Filename: i.Filename, SizeBytes: i.SizeBytes, Status: i.Status,
				Encoding: i.Encoding, Delimiter: i.Delimiter, Headers: i.Headers, MappingSource: i.MappingSource, Mapping: i.Mapping,
				Options: i.Options, Stats: i.Stats, CreatedAt: i.CreatedAt, CommittedAt: i.CommittedAt})
		}
		out["imports.json"] = imports

		inv, err := exportInvestments(ctx, q, userID)
		if err != nil {
			return err
		}
		out["investments.json"] = inv
		return nil
	})
	return out, dbErr("export", err)
}

func exportInvestments(ctx context.Context, q *db.Queries, userID uuid.UUID) (*ExportInvestments, error) {
	p, err := q.GetInvestmentProfile(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil // never saved: null in the export
	}
	if err != nil {
		return nil, err
	}
	out := &ExportInvestments{SwrBp: int(p.SwrBp), RealReturnBp: int(p.RealReturnBp)}
	out.PPK.EmployeeMinor, out.PPK.EmployerMinor, out.PPK.StateMinor, out.PPK.ValueMinor = p.PpkEmployeeMinor, p.PpkEmployerMinor, p.PpkStateMinor, p.PpkValueMinor
	pos, err := q.ListInvestmentPositions(ctx, userID)
	if err != nil {
		return nil, err
	}
	out.Positions = make([]struct {
		ID                uuid.UUID `json:"id"`
		Name              string    `json:"name"`
		Type              string    `json:"type"`
		InvestedMinor     int64     `json:"investedMinor"`
		ValueMinor        int64     `json:"valueMinor"`
		AnnualIncomeMinor int64     `json:"annualIncomeMinor"`
	}, len(pos))
	for i := range pos {
		x := &pos[i]
		out.Positions[i].ID, out.Positions[i].Name, out.Positions[i].Type = x.ID, x.Name, x.Type
		out.Positions[i].InvestedMinor, out.Positions[i].ValueMinor, out.Positions[i].AnnualIncomeMinor = x.InvestedMinor, x.ValueMinor, x.AnnualIncomeMinor
	}
	return out, nil
}

// ExportFiles calls fn with each import and its original bytes, oldest first, one at a time so
// only one file is in memory.
func (l *Ledger) ExportFiles(ctx context.Context, userID uuid.UUID, fn func(Import, []byte) error) error {
	var ids []uuid.UUID
	if err := l.st.WithUserReadOnly(ctx, userID, func(q *db.Queries, _ pgx.Tx) (err error) {
		ids, err = q.ExportImportIDs(ctx, userID)
		return err
	}); err != nil {
		return dbErr("export files", err)
	}
	for _, id := range ids {
		imp, content, err := l.importWithFile(ctx, userID, id)
		if errors.Is(err, platform.ErrNotFound) || errors.Is(err, ErrNotFound) {
			continue // deleted meanwhile
		}
		if err != nil {
			return err
		}
		if err := fn(imp, content); err != nil {
			return err
		}
	}
	return nil
}
