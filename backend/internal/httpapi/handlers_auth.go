package httpapi

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/auth"
	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/gen"
	mw "github.com/x2db736ac85h/finuance/backend/internal/httpapi/middleware"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// limits are the rate limiters of the auth routes (§6.1, ADR 0012).
type limits struct {
	loginIP      *mw.Limiter // signup + login per client IP
	loginIPEmail *mw.Limiter // signup + login per (IP, email)
	password     *mw.Limiter // password checks per signed-in user
	upload       *mw.Limiter // uploads per user
	assistant    *mw.Limiter // assistant questions per user (FINUANCE_AI_RATE_PER_HOUR)
}

func newLimits(clock platform.Clock, aiPerHour int) limits {
	if aiPerHour < 1 {
		aiPerHour = 30
	}
	return limits{
		loginIP:      mw.NewLimiter(mw.LoginPerIP, clock, 0),
		loginIPEmail: mw.NewLimiter(mw.LoginPerIPEmail, clock, 0),
		password:     mw.NewLimiter(mw.PasswordPerUser, clock, 0),
		upload:       mw.NewLimiter(mw.UploadPerUser, clock, 0),
		assistant:    mw.NewLimiter(mw.Policy{Events: aiPerHour, Per: time.Hour}, clock, 0),
	}
}

// credentialAttempt applies both login limits before any password work is done.
func (a *api) credentialAttempt(ctx context.Context, email string) error {
	ip := mw.FromContext(ctx).ClientIP
	if err := a.limits.loginIP.Allow(ip); err != nil {
		return err
	}
	return a.limits.loginIPEmail.Allow(ip + "|" + strings.ToLower(strings.TrimSpace(email)))
}

func userDTO(u auth.User) gen.User {
	return gen.User{Id: u.ID, Email: u.Email, DisplayName: u.DisplayName, IsDemo: u.IsDemo, CreatedAt: u.CreatedAt}
}

// Signup creates an account and signs it in.
func (a *api) Signup(ctx context.Context, req gen.SignupRequestObject) (gen.SignupResponseObject, error) {
	if err := a.credentialAttempt(ctx, req.Body.Email); err != nil {
		return nil, err
	}
	in := auth.SignupInput{Email: req.Body.Email, Password: req.Body.Password}
	if req.Body.DisplayName != nil {
		in.DisplayName = *req.Body.DisplayName
	}
	u, token, err := a.auth.Signup(ctx, in, authFrom(ctx).client)
	if err != nil {
		return nil, err
	}
	return gen.Signup201JSONResponse{Body: userDTO(u), Headers: gen.Signup201ResponseHeaders{SetCookie: a.cookies.session(token)}}, nil
}

// Login signs in, rotating any session the browser already had.
func (a *api) Login(ctx context.Context, req gen.LoginRequestObject) (gen.LoginResponseObject, error) {
	if err := a.credentialAttempt(ctx, req.Body.Email); err != nil {
		return nil, err
	}
	ra := authFrom(ctx)
	u, token, err := a.auth.Login(ctx, req.Body.Email, req.Body.Password, ra.token, ra.client)
	var failed *auth.FailedLoginError
	if errors.As(err, &failed) { // an existing account; an unknown email has no log to write to
		a.audit(ctx, failed.UserID, ledger.AuditLoginFailed, "", nil)
	}
	if err != nil {
		return nil, err
	}
	a.audit(ctx, u.ID, ledger.AuditLogin, "", nil)
	return gen.Login200JSONResponse{Body: userDTO(u), Headers: gen.Login200ResponseHeaders{SetCookie: a.cookies.session(token)}}, nil
}

// Logout deletes the session (if any) and clears the cookie.
func (a *api) Logout(ctx context.Context, _ gen.LogoutRequestObject) (gen.LogoutResponseObject, error) {
	token := authFrom(ctx).token
	var who uuid.UUID // logout is a public route; the session, if still valid, says whose it was
	if token != "" {
		if id, err := a.auth.Resolve(ctx, token); err == nil {
			who = id.User.ID
		}
	}
	if err := a.auth.Logout(ctx, token); err != nil {
		return nil, err
	}
	if who != uuid.Nil {
		a.audit(ctx, who, ledger.AuditLogout, "", nil)
	}
	return gen.Logout204Response{Headers: gen.Logout204ResponseHeaders{SetCookie: a.cookies.clear()}}, nil
}
