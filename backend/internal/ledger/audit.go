package ledger

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// The audit log (PLAN task 17.6, migration 00010): what a user did that matters for their
// account's security, shown to them on Settings → Activity. Rows are append-only for the app and
// kept AuditRetention.

// Audited actions. The web app words them; keep the two lists equal (TestAuditActionsAreKnown).
const (
	AuditLogin            = "auth.login"
	AuditLoginFailed      = "auth.login_failed"
	AuditLogout           = "auth.logout"
	AuditPasswordChanged  = "auth.password_changed"
	AuditSessionRevoked   = "auth.session_revoked"
	AuditImportCommitted  = "import.committed"
	AuditImportDeleted    = "import.deleted"
	AuditAccountDeleted   = "account.deleted"
	AuditExportCSV        = "export.csv"
	AuditExportData       = "export.data"
	AuditRulesApplied     = "rules.applied"
	AuditLegacyImport     = "legacy.import_started"
	AuditDuplicateDecided = "duplicates.decided"
	AuditDuplicateReset   = "duplicates.reset"
	AuditAssistantApplied = "assistant.action_applied"
)

// AuditActions lists every action above.
var AuditActions = []string{AuditLogin, AuditLoginFailed, AuditLogout, AuditPasswordChanged, AuditSessionRevoked,
	AuditImportCommitted, AuditImportDeleted, AuditAccountDeleted, AuditExportCSV, AuditExportData,
	AuditRulesApplied, AuditLegacyImport, AuditDuplicateDecided, AuditDuplicateReset, AuditAssistantApplied}

// AuditRetention is how long events are kept; the table's delete policy says the same.
const AuditRetention = 365 * 24 * time.Hour

// AuditEvent is one action to record. IP and Browser come from the request (zero for jobs); Meta
// holds small facts (counts, kinds), never transaction fields.
type AuditEvent struct {
	Action   string
	EntityID string // the affected record's id, if any
	IP       netip.Addr
	Browser  string // a short label such as "Firefox on Linux" (Browser), not the raw User-Agent
	Meta     map[string]any
}

// AuditEntry is a recorded event.
type AuditEntry struct {
	ID       int64
	At       time.Time
	Action   string
	EntityID string
	IP       netip.Addr // zero when the action had no request (a job)
	Browser  string
	Meta     map[string]any
}

// RecordAudit stores an event of the user.
func (l *Ledger) RecordAudit(ctx context.Context, userID uuid.UUID, ev AuditEvent) error {
	return l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		return insertAudit(ctx, q, userID, ev)
	})
}

// insertAudit stores an event inside the caller's transaction, so it commits with the change.
func insertAudit(ctx context.Context, q *db.Queries, userID uuid.UUID, ev AuditEvent) error {
	meta := []byte("{}")
	if len(ev.Meta) > 0 {
		b, err := json.Marshal(ev.Meta)
		if err != nil {
			return fmt.Errorf("%w: audit meta: %w", ErrInvalid, err)
		}
		meta = b
	}
	p := db.InsertAuditEventParams{UserID: userID, Action: ev.Action, Browser: truncateRunes(ev.Browser, 64), Meta: meta}
	if ev.EntityID != "" {
		p.EntityID = &ev.EntityID
	}
	if ev.IP.IsValid() {
		ip := ev.IP.Unmap()
		p.Ip = &ip
	}
	return q.InsertAuditEvent(ctx, p)
}

// AuditPage returns up to limit events of the user, newest first, older than beforeID (0: from
// the newest).
func (l *Ledger) AuditPage(ctx context.Context, userID uuid.UUID, beforeID int64, limit int32) ([]AuditEntry, error) {
	var rows []db.ListAuditEventsRow
	err := l.st.WithUser(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		var err error
		rows, err = q.ListAuditEvents(ctx, db.ListAuditEventsParams{UserID: userID, BeforeID: beforeID, PageSize: limit})
		return err
	})
	if err != nil {
		return nil, err
	}
	out := make([]AuditEntry, len(rows))
	for i := range rows {
		out[i] = auditEntry(rows[i].ID, rows[i].At, rows[i].Action, rows[i].EntityID, rows[i].Ip, rows[i].Browser, rows[i].Meta)
	}
	return out, nil
}

// AppliedProposals returns the keys of the chat's assistant proposals the user applied (recorded
// as AuditAssistantApplied with the chat as the entity).
func (l *Ledger) AppliedProposals(ctx context.Context, userID, chatID uuid.UUID) ([]string, error) {
	var keys []string
	err := l.st.WithUserReadOnly(ctx, userID, func(q *db.Queries, _ pgx.Tx) error {
		var err error
		keys, err = q.AppliedProposals(ctx, db.AppliedProposalsParams{UserID: userID, ChatID: chatID.String()})
		return err
	})
	return keys, err
}

func auditEntry(id int64, at time.Time, action string, entity *string, ip *netip.Addr, browser string, meta []byte) AuditEntry {
	e := AuditEntry{ID: id, At: at, Action: action, Browser: browser}
	if entity != nil {
		e.EntityID = *entity
	}
	if ip != nil {
		e.IP = *ip
	}
	if len(meta) > 0 && string(meta) != "{}" {
		_ = json.Unmarshal(meta, &e.Meta) // the column only holds objects
	}
	return e
}

// CleanupAudit deletes every user's events older than AuditRetention, one user at a time under
// that user's RLS context (users is not under RLS; audit_log is).
func (l *Ledger) CleanupAudit(ctx context.Context) (int64, error) {
	var total int64
	after := uuid.Nil
	for {
		var ids []uuid.UUID
		if err := l.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
			var err error
			ids, err = q.ListUserIDsAfter(ctx, db.ListUserIDsAfterParams{After: after, PageSize: 500})
			return err
		}); err != nil {
			return total, err
		}
		for _, id := range ids {
			if err := ctx.Err(); err != nil {
				return total, err
			}
			if err := l.st.WithUser(ctx, id, func(q *db.Queries, _ pgx.Tx) error {
				n, err := q.DeleteExpiredAuditEvents(ctx, id)
				total += n
				return err
			}); err != nil {
				return total, err
			}
		}
		if len(ids) < 500 {
			return total, nil
		}
		after = ids[len(ids)-1]
	}
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
