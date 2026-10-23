package httpapi

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/x2db736ac85h/finuance/backend/internal/assistant"
	"github.com/x2db736ac85h/finuance/backend/internal/auth"
	mw "github.com/x2db736ac85h/finuance/backend/internal/httpapi/middleware"
	"github.com/x2db736ac85h/finuance/backend/internal/insights"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/query"

	"github.com/x2db736ac85h/finuance/backend/internal/config"
	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/gen"
)

// api implements gen.StrictServerInterface.
type api struct {
	cfg      config.Config
	version  string
	prod     bool
	auth     *auth.Service
	ledger   *ledger.Ledger
	query    *query.Engine
	insights *insights.Service
	conns    *assistant.Connections
	jobs     *river.Client[pgx.Tx]
	cookies  cookies
	limits   limits
}

var _ gen.StrictServerInterface = (*api)(nil)

// GetMeta returns public instance information.
func (a *api) GetMeta(context.Context, gen.GetMetaRequestObject) (gen.GetMetaResponseObject, error) {
	return gen.GetMeta200JSONResponse{Version: a.version, SignupEnabled: a.cfg.AllowSignup, DemoMode: a.cfg.DemoMode,
		AssistantEnabled: a.conns.Enabled()}, nil
}

// requestID returns the request id for problem bodies built by a handler.
func requestID(ctx context.Context) *string {
	if id := mw.RequestID(ctx); id != "" {
		return &id
	}
	return nil
}
