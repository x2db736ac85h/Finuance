package httpapi

import (
	"context"
	"errors"

	"github.com/x2db736ac85h/finuance/backend/internal/auth"
	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
)

// Two-step sign-in (PLAN 23.7, ADR 0040). The routes under /me/second-factor need a session (no
// access token reaches them, tokens.go) and each counts against the limiter for password checks.

// adminsNeedSecondFactor reads the instance's rule; an instance without administration has none.
func (a *api) adminsNeedSecondFactor(ctx context.Context) bool {
	if a.admin == nil {
		return false
	}
	s, _, err := a.admin.Settings(ctx)
	return err == nil && s.AdminsNeedSecondFactor()
}

// LoginSecondStep answers the challenge of a sign-in with a code and starts the session.
func (a *api) LoginSecondStep(ctx context.Context, req gen.LoginSecondStepRequestObject) (gen.LoginSecondStepResponseObject, error) {
	ra := authFrom(ctx)
	if err := a.limits.loginIP.Allow(ra.client.IP.String()); err != nil {
		return nil, err
	}
	// per user as well: a challenge takes five codes, and new challenges cost only the password
	userID, err := a.auth.ChallengeUser(ctx, req.Body.Challenge)
	if err != nil {
		return nil, err
	}
	if err := a.limits.secondStep.Allow(userID.String()); err != nil {
		return nil, err
	}
	u, token, recovery, err := a.auth.CompleteLogin(ctx, req.Body.Challenge, req.Body.Code, ra.token, ra.client)
	var failed *auth.FailedSecondStepError
	if errors.As(err, &failed) {
		a.audit(ctx, failed.UserID, ledger.AuditSecondStepFailed, "", nil)
	}
	if err != nil {
		return nil, err
	}
	a.audit(ctx, u.ID, ledger.AuditLogin, "", nil)
	if recovery {
		left, _ := a.auth.SecondFactorOf(ctx, u.ID)
		a.audit(ctx, u.ID, ledger.AuditRecoveryCodeUsed, "", map[string]any{"left": left.RecoveryCodes})
	}
	return gen.LoginSecondStep200JSONResponse{Body: a.userDTO(ctx, u), Headers: gen.LoginSecondStep200ResponseHeaders{SetCookie: a.cookies.session(token)}}, nil
}

// GetSecondFactor reports whether two-step sign-in is on for the account.
func (a *api) GetSecondFactor(ctx context.Context, _ gen.GetSecondFactorRequestObject) (gen.GetSecondFactorResponseObject, error) {
	id := identity(ctx)
	f, err := a.auth.SecondFactorOf(ctx, id.User.ID)
	if err != nil {
		return nil, err
	}
	return gen.GetSecondFactor200JSONResponse{Enabled: f.Enabled, ConfirmedAt: f.ConfirmedAt, RecoveryCodes: int32(f.RecoveryCodes), //nolint:gosec // at most ten
		Available:         len(a.cfg.InstanceKey) > 0 && !id.User.IsDemo && !a.cfg.DemoMode,
		RequiredForAdmins: a.adminsNeedSecondFactor(ctx)}, nil
}

// StartSecondFactor begins enrolment and returns the secret for the authenticator app.
func (a *api) StartSecondFactor(ctx context.Context, _ gen.StartSecondFactorRequestObject) (gen.StartSecondFactorResponseObject, error) {
	id := identity(ctx)
	if err := a.limits.password.Allow(id.User.ID.String()); err != nil {
		return nil, err
	}
	e, err := a.auth.StartSecondFactor(ctx, id)
	if err != nil {
		return nil, err
	}
	return gen.StartSecondFactor200JSONResponse{Secret: e.Secret, Uri: e.URI}, nil
}

// ConfirmSecondFactor switches two-step sign-in on and returns the recovery codes.
func (a *api) ConfirmSecondFactor(ctx context.Context, req gen.ConfirmSecondFactorRequestObject) (gen.ConfirmSecondFactorResponseObject, error) {
	id := identity(ctx)
	if err := a.limits.password.Allow(id.User.ID.String()); err != nil {
		return nil, err
	}
	codes, err := a.auth.ConfirmSecondFactor(ctx, id, req.Body.Code)
	if err != nil {
		return nil, err
	}
	a.auditMe(ctx, ledger.AuditSecondFactorEnabled, "", nil)
	return gen.ConfirmSecondFactor200JSONResponse{RecoveryCodes: codes}, nil
}

// NewRecoveryCodes replaces the recovery codes after a code from the app.
func (a *api) NewRecoveryCodes(ctx context.Context, req gen.NewRecoveryCodesRequestObject) (gen.NewRecoveryCodesResponseObject, error) {
	id := identity(ctx)
	if err := a.limits.password.Allow(id.User.ID.String()); err != nil {
		return nil, err
	}
	codes, err := a.auth.NewRecoveryCodesFor(ctx, id, req.Body.Code)
	if err != nil {
		return nil, err
	}
	a.auditMe(ctx, ledger.AuditRecoveryCodesReplaced, "", nil)
	return gen.NewRecoveryCodes200JSONResponse{RecoveryCodes: codes}, nil
}

// DisableSecondFactor switches two-step sign-in off after the password and a code.
func (a *api) DisableSecondFactor(ctx context.Context, req gen.DisableSecondFactorRequestObject) (gen.DisableSecondFactorResponseObject, error) {
	id := identity(ctx)
	if err := a.limits.password.Allow(id.User.ID.String()); err != nil {
		return nil, err
	}
	if err := a.auth.DisableSecondFactor(ctx, id, req.Body.Password, req.Body.Code); err != nil {
		return nil, err
	}
	a.auditMe(ctx, ledger.AuditSecondFactorDisabled, "", nil)
	return gen.DisableSecondFactor204Response{}, nil
}

// AdminRemoveSecondFactor removes another account's two-step sign-in.
func (a *api) AdminRemoveSecondFactor(ctx context.Context, req gen.AdminRemoveSecondFactorRequestObject) (gen.AdminRemoveSecondFactorResponseObject, error) {
	u, err := a.admin.GetUser(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	if err := a.admin.RemoveSecondFactor(ctx, identity(ctx).User.ID, req.Id); err != nil {
		return nil, err
	}
	a.adminAudit(ctx, ledger.AuditAdminSecondFactorRemoved, &u, nil)
	return gen.AdminRemoveSecondFactor204Response{}, nil
}
