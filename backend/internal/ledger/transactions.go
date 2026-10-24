package ledger

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/ingest"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// Transaction is the detail view of one transaction.
type Transaction struct {
	ID                  uuid.UUID
	AccountID           uuid.UUID
	Origin              string // import | manual
	BookedOn            platform.Date
	ValueOn             *platform.Date
	AmountMinor         int64
	Currency            string
	Payee               string
	PayeeGroup          string
	Description         string
	Reference           string
	TxType              string
	BankCategory        string
	CounterpartyAccount string
	BalanceAfterMinor   *int64
	CategoryID          *uuid.UUID
	CategoryName        string
	CategorySource      string // none | bank | rule | manual
	GroupID             *uuid.UUID
	GroupName           string
	GroupColor          *int
	Note                string
	Raw                 []ingest.RawField
	SourceImportID      *uuid.UUID
	Occurrence          int
	IsTransfer          bool
	TransferSource      string // none | pair | rule | manual (ADR 0018)
	TransferPeer        *TransferPeer
	CategoryRuleID      *uuid.UUID
	CategoryRuleName    string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// TransferPeer is the other half of a detected transfer pair.
type TransferPeer struct {
	ID          uuid.UUID
	AccountName string
	BookedOn    platform.Date
	AmountMinor int64
}

// GetTransaction returns one transaction with its category and group names.
func (l *Ledger) GetTransaction(ctx context.Context, userID, id uuid.UUID) (Transaction, error) {
	var out Transaction
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		t, err := q.GetTransaction(ctx, db.GetTransactionParams{UserID: userID, ID: id})
		if err != nil {
			return err
		}
		out = Transaction{ID: t.ID, AccountID: t.AccountID, Origin: t.Origin, BookedOn: t.BookedOn, ValueOn: t.ValueOn,
			AmountMinor: t.AmountMinor, Currency: t.Currency, Payee: t.Payee, PayeeGroup: t.PayeeGroup,
			Description: t.Description, Reference: t.Reference, TxType: t.TxType, BankCategory: t.BankCategory,
			CounterpartyAccount: t.CounterpartyAccount, BalanceAfterMinor: t.BalanceAfterMinor, CategoryID: t.CategoryID,
			CategoryName: deref(t.CategoryName), CategorySource: t.CategorySource, GroupID: t.GroupID,
			GroupName: deref(t.GroupName), Note: t.Note, SourceImportID: t.SourceImportID,
			Occurrence: int(t.Occurrence), IsTransfer: t.IsTransfer, TransferSource: t.TransferSource,
			CategoryRuleID: t.CategoryRuleID, CategoryRuleName: deref(t.RuleName), CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt}
		if t.TransferPeerID != nil && t.PeerBookedOn != nil && t.PeerAmountMinor != nil {
			out.TransferPeer = &TransferPeer{ID: *t.TransferPeerID, AccountName: deref(t.PeerAccountName),
				BookedOn: *t.PeerBookedOn, AmountMinor: *t.PeerAmountMinor}
		}
		if t.GroupColor != nil {
			c := int(*t.GroupColor)
			out.GroupColor = &c
		}
		out.Raw, err = decodeRaw(t.Raw)
		return err
	})
	return out, dbErr("get transaction", err)
}

// Provenance is one import row a transaction was seen in.
type Provenance struct {
	ImportID    uuid.UUID
	RowIndex    int
	Filename    string
	CommittedAt *time.Time
	Raw         []ingest.RawField
}

// TransactionProvenance lists the import rows linked to a transaction, oldest import first.
func (l *Ledger) TransactionProvenance(ctx context.Context, userID, id uuid.UUID) ([]Provenance, error) {
	var out []Provenance
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		rows, err := q.TransactionProvenance(ctx, db.TransactionProvenanceParams{UserID: userID, TransactionID: &id})
		if err != nil {
			return err
		}
		for _, r := range rows {
			p := Provenance{ImportID: r.ImportID, RowIndex: int(r.RowIndex), Filename: r.Filename, CommittedAt: r.CommittedAt}
			if p.Raw, err = decodeRaw(r.Raw); err != nil {
				return err
			}
			out = append(out, p)
		}
		return nil
	})
	return out, dbErr("transaction provenance", err)
}

// TransactionPatch is a single-transaction edit (PATCH /transactions/{id}). Category: at most one of
// CategoryID, CategoryName (created when missing) or ResetCategory (back to the bank's category).
type TransactionPatch struct {
	CategoryID    *uuid.UUID
	CategoryName  *string
	ResetCategory bool
	Note          *string
}

// UpdateTransaction applies p as an annotation (so the edit survives a re-import) and returns the
// transaction. A set category becomes a manual one.
func (l *Ledger) UpdateTransaction(ctx context.Context, userID, id uuid.UUID, p TransactionPatch) (Transaction, error) {
	n := 0
	for _, set := range []bool{p.CategoryID != nil, p.CategoryName != nil, p.ResetCategory} {
		if set {
			n++
		}
	}
	if n > 1 {
		return Transaction{}, invalidf("set only one of categoryId, categoryName and resetCategory")
	}
	if n == 0 && p.Note == nil {
		return Transaction{}, invalidf("nothing to change")
	}
	if _, err := l.GetTransaction(ctx, userID, id); err != nil {
		return Transaction{}, err
	}
	patch := AnnotationPatch{SetCategory: n == 1, CategoryID: p.CategoryID}
	if p.CategoryName != nil {
		name, err := cleanName("category", *p.CategoryName, maxCategoryName)
		if err != nil {
			return Transaction{}, err
		}
		ids, err := l.EnsureCategories(ctx, userID, []string{name})
		if err != nil {
			return Transaction{}, err
		}
		cid, ok := ids[nameKey(name)]
		if !ok { // the name of a deleted category: typing it asks for that category again
			c, err := l.CreateCategory(ctx, userID, name)
			if err != nil {
				return Transaction{}, err
			}
			cid = c.ID
		}
		patch.CategoryID = &cid
	}
	if p.Note != nil {
		patch.SetNote, patch.Note = true, strings.TrimSpace(*p.Note)
	}
	if _, err := l.Annotate(ctx, userID, []uuid.UUID{id}, patch); err != nil {
		return Transaction{}, err
	}
	return l.GetTransaction(ctx, userID, id)
}
