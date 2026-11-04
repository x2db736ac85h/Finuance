package httpapi

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/x2db736ac85h/finuance/backend/internal/admin"
	"github.com/x2db736ac85h/finuance/backend/internal/assistant"
	"github.com/x2db736ac85h/finuance/backend/internal/auth"
	mw "github.com/x2db736ac85h/finuance/backend/internal/httpapi/middleware"
	"github.com/x2db736ac85h/finuance/backend/internal/insights"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/legacyimport"
	"github.com/x2db736ac85h/finuance/backend/internal/notify"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
	"github.com/x2db736ac85h/finuance/backend/internal/rules"

	"github.com/x2db736ac85h/finuance/backend/internal/config"
	"github.com/x2db736ac85h/finuance/backend/internal/friends"
	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/gen"
)

// api implements gen.StrictServerInterface.
type api struct {
	cfg      config.Config
	version  string
	prod     bool
	auth     *auth.Service
	admin    *admin.Service
	friends  *friends.Service
	notify   *notify.Service
	ledger   *ledger.Ledger
	legacy   *legacyimport.Migrator
	rules    *rules.Service
	query    *query.Engine
	insights *insights.Service
	ai       *assistant.Assistant
	jobs     *river.Client[pgx.Tx]
	cookies  cookies
	limits   limits
}

var _ gen.StrictServerInterface = (*api)(nil)

// GetMeta returns public instance information: what the instance offers, the view defaults and
// the themes an administrator approved.
func (a *api) GetMeta(ctx context.Context, _ gen.GetMetaRequestObject) (gen.GetMetaResponseObject, error) {
	f := a.features(ctx)
	out := gen.GetMeta200JSONResponse{Version: a.version, SignupEnabled: a.cfg.AllowSignup, DemoMode: a.cfg.DemoMode,
		AssistantEnabled: a.conns().Enabled() && f.Assistant, Themes: []gen.PublicTheme{},
		Features: gen.Features{Overview: f.Overview, Assistant: f.Assistant, Friends: f.Friends, SharedGroups: f.SharedGroups}}
	if a.admin != nil {
		pub, err := a.admin.Public(ctx)
		if err != nil {
			return nil, err
		}
		raw, _ := json.Marshal(pub.Defaults) // same field names as the API's ViewDefaults
		if err := json.Unmarshal(raw, &out.Defaults); err != nil {
			return nil, err
		}
		for _, t := range pub.Themes {
			out.Themes = append(out.Themes, gen.PublicTheme{Id: t.Slug, Name: t.Name, Tokens: themeTokensDTO(t.Tokens)})
		}
	}
	return out, nil
}

// requestID returns the request id for problem bodies built by a handler.
func requestID(ctx context.Context) *string {
	if id := mw.RequestID(ctx); id != "" {
		return &id
	}
	return nil
}
