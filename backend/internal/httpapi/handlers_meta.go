package httpapi

import (
	"context"

	"github.com/x2db736ac85h/finuance/backend/internal/config"
	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/gen"
)

// api implements gen.StrictServerInterface.
type api struct {
	cfg     config.Config
	version string
}

var _ gen.StrictServerInterface = (*api)(nil)

// GetMeta returns public instance information.
func (a *api) GetMeta(context.Context, gen.GetMetaRequestObject) (gen.GetMetaResponseObject, error) {
	return gen.GetMeta200JSONResponse{Version: a.version, SignupEnabled: a.cfg.AllowSignup, DemoMode: a.cfg.DemoMode}, nil
}
