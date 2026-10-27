package httpapi

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/observability"
)

// audit records an action of userID in the audit log (PLAN 17.6) with the request's address and a
// short browser label. It runs after the action succeeded; a failure to record is logged and not
// returned, because the change it describes has already happened.
func (a *api) audit(ctx context.Context, userID uuid.UUID, action, entityID string, meta map[string]any) {
	c := authFrom(ctx).client
	ev := ledger.AuditEvent{Action: action, EntityID: entityID, IP: c.IP, Browser: browserLabel(c.UserAgent), Meta: meta}
	if err := a.ledger.RecordAudit(ctx, userID, ev); err != nil {
		observability.Logger(ctx).WarnContext(ctx, "audit event not recorded", "action", action, "error", err)
	}
}

// auditMe records an action of the signed-in user.
func (a *api) auditMe(ctx context.Context, action, entityID string, meta map[string]any) {
	a.audit(ctx, identity(ctx).User.ID, action, entityID, meta)
}

// browserLabel turns a User-Agent into "Firefox on Linux": enough to recognise a device on the
// Activity page, without storing the full string (a fingerprinting aid).
func browserLabel(ua string) string {
	if strings.TrimSpace(ua) == "" {
		return ""
	}
	name := "Other browser"
	switch {
	case strings.Contains(ua, "Edg/") || strings.Contains(ua, "EdgA/") || strings.Contains(ua, "EdgiOS/"):
		name = "Edge"
	case strings.Contains(ua, "OPR/"):
		name = "Opera"
	case strings.Contains(ua, "Firefox/") || strings.Contains(ua, "FxiOS/"):
		name = "Firefox"
	case strings.Contains(ua, "Chrome/") || strings.Contains(ua, "CriOS/"):
		name = "Chrome"
	case strings.Contains(ua, "Safari/"):
		name = "Safari"
	case strings.HasPrefix(ua, "curl/"):
		name = "curl"
	}
	var os string
	switch { // iOS and Android name Mac OS X and Linux too, so they come first
	case strings.Contains(ua, "iPhone") || strings.Contains(ua, "iPad"):
		os = "iOS"
	case strings.Contains(ua, "Android"):
		os = "Android"
	case strings.Contains(ua, "Windows"):
		os = "Windows"
	case strings.Contains(ua, "Macintosh") || strings.Contains(ua, "Mac OS X"):
		os = "macOS"
	case strings.Contains(ua, "CrOS"):
		os = "ChromeOS"
	case strings.Contains(ua, "Linux"):
		os = "Linux"
	}
	if os == "" {
		return name
	}
	return name + " on " + os
}
