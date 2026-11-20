package httpapi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/auth"
	"github.com/x2db736ac85h/finuance/backend/internal/config"
	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/gen"
	mw "github.com/x2db736ac85h/finuance/backend/internal/httpapi/middleware"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// limits are the rate limiters of the auth routes (§6.1, ADR 0012).
type limits struct {
	loginIP      *mw.Limiter // signup + login per client IP
	loginIPEmail *mw.Limiter // signup + login per (IP, email)
	loginFailed  *mw.Limiter // failed sign-ins per e-mail address, from any client (API-1)
	password     *mw.Limiter // password checks per signed-in user
	secondStep   *mw.Limiter // codes at the second step of signing in, per user (ADR 0040)
	upload       *mw.Limiter // uploads per user
	reparse      *mw.Limiter // re-parsing a stored file, per user (IMP-4)
	assistant    *mw.Limiter // assistant questions per user (FINUANCE_AI_RATE_PER_HOUR)
	// per-user rates an administrator set below the server's: one limiter per rate
	assistantMu    *sync.Mutex
	assistantRates map[int64]*mw.Limiter
	clock          platform.Clock
	aiPerHour      int64
	demo           *mw.Limiter // new demo users per client IP
	demoAsk        *mw.Limiter // assistant questions per demo user over its lifetime
	friendUser     *mw.Limiter // friend codes entered per user (ADR 0027)
	friendIP       *mw.Limiter // friend codes entered per client IP
	sharedWrite    *mw.Limiter // expenses, payments and member changes in shared groups, per user (API-5)
	sharedInvite   *mw.Limiter // invitations sent, per user
	statusRefresh  *mw.Limiter // forced re-checks of the model servers, per user (AI-5)
}

func newLimits(clock platform.Clock, aiPerHour int) limits {
	if aiPerHour < 1 {
		aiPerHour = 30
	}
	return limits{
		loginIP:        mw.NewLimiter(mw.LoginPerIP, clock, 0),
		loginIPEmail:   mw.NewLimiter(mw.LoginPerIPEmail, clock, 0),
		loginFailed:    mw.NewLimiter(mw.LoginFailuresPerEmail, clock, 0),
		password:       mw.NewLimiter(mw.PasswordPerUser, clock, 0),
		secondStep:     mw.NewLimiter(mw.SecondStepPerUser, clock, 0),
		upload:         mw.NewLimiter(mw.UploadPerUser, clock, 0),
		reparse:        mw.NewLimiter(mw.ReparsePerUser, clock, 0),
		assistant:      mw.NewLimiter(mw.Policy{Events: aiPerHour, Per: time.Hour}, clock, 0),
		assistantMu:    &sync.Mutex{},
		assistantRates: map[int64]*mw.Limiter{},
		clock:          clock,
		aiPerHour:      int64(aiPerHour),
		demo:           mw.NewLimiter(mw.DemoPerIP, clock, 0),
		friendUser:     mw.NewLimiter(mw.FriendCodesPerHour, clock, 0),
		friendIP:       mw.NewLimiter(mw.FriendCodesPerHour, clock, 0),
		sharedWrite:    mw.NewLimiter(mw.SharedWritesPerUser, clock, 0),
		sharedInvite:   mw.NewLimiter(mw.SharedInvitesPerUser, clock, 0),
		statusRefresh:  mw.NewLimiter(mw.StatusRefreshPerUser, clock, 0),
		// A full bucket and one token back per lifetime + 1 h: exactly DemoAssistantMessages
		// before the user is deleted.
		demoAsk: mw.NewLimiter(mw.Policy{Events: config.DemoAssistantMessages,
			Per: config.DemoAssistantMessages * (auth.DemoLifetime + time.Hour)}, clock, 0),
	}
}

// emailKey is an e-mail address as the limiters key it.
func emailKey(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

// credentialAttempt applies the login limits before any password work is done: per client (an
// IPv6 client is its /64) and per client and e-mail address.
func (a *api) credentialAttempt(ctx context.Context, email string) error {
	ip := mw.ClientKey(mw.FromContext(ctx).ClientIP)
	if err := a.limits.loginIP.Allow(ip); err != nil {
		return err
	}
	return a.limits.loginIPEmail.Allow(ip + "|" + emailKey(email))
}

func (a *api) userDTO(ctx context.Context, u auth.User) gen.User {
	l := a.effectiveLimits(ctx, u)
	return gen.User{Id: u.ID, Email: u.Email, DisplayName: u.DisplayName, IsDemo: u.IsDemo, CreatedAt: u.CreatedAt,
		IsAdmin: u.IsAdmin, MustChangePassword: u.MustChangePassword, SecondFactor: u.SecondFactor,
		SecondFactorRequired: u.IsAdmin && !u.SecondFactor && a.adminsNeedSecondFactor(ctx),
		Limits: gen.EffectiveLimits{MaxUploadBytes: l.MaxUploadBytes, ImportsPerDay: l.ImportsPerDay, MaxAccounts: l.MaxAccounts,
			MaxGroups: l.MaxGroups, MaxSharedGroups: l.MaxSharedGroups, AssistantPerHour: l.AssistantPerHour,
			AssistantTokensPerDay: l.AssistantTokensPerDay}}
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
	return gen.Signup201JSONResponse{Body: a.userDTO(ctx, u), Headers: gen.Signup201ResponseHeaders{SetCookie: a.cookies.session(token)}}, nil
}

// Login signs in, rotating any session the browser already had.
func (a *api) Login(ctx context.Context, req gen.LoginRequestObject) (gen.LoginResponseObject, error) {
	if err := a.credentialAttempt(ctx, req.Body.Email); err != nil {
		return nil, err
	}
	// too many failed sign-ins for this address lately, from anywhere: no further guess is checked
	if err := a.limits.loginFailed.Check(emailKey(req.Body.Email)); err != nil {
		return nil, err
	}
	ra := authFrom(ctx)
	u, token, err := a.auth.Login(ctx, req.Body.Email, req.Body.Password, ra.token, ra.client)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		// counted for unknown addresses too, so the limit doesn't tell which accounts exist
		_ = a.limits.loginFailed.Allow(emailKey(req.Body.Email))
	}
	var failed *auth.FailedLoginError
	if errors.As(err, &failed) { // an existing account; an unknown email has no log to write to
		a.audit(ctx, failed.UserID, ledger.AuditLoginFailed, "", nil)
	}
	var second *auth.SecondStepRequiredError
	if errors.As(err, &second) { // the password was right; the session starts with the code
		return gen.Login202JSONResponse{Challenge: second.Challenge, ExpiresAt: second.ExpiresAt}, nil
	}
	if err != nil {
		return nil, err
	}
	a.audit(ctx, u.ID, ledger.AuditLogin, "", nil)
	return gen.Login200JSONResponse{Body: a.userDTO(ctx, u), Headers: gen.Login200ResponseHeaders{SetCookie: a.cookies.session(token)}}, nil
}

// StartDemo signs a visitor in as a new demo user with the demo year imported (PLAN 19.1). Only
// on a demo instance; elsewhere the route doesn't exist.
func (a *api) StartDemo(ctx context.Context, _ gen.StartDemoRequestObject) (gen.StartDemoResponseObject, error) {
	if !a.cfg.DemoMode {
		return nil, fmt.Errorf("/auth/demo: %w", platform.ErrNotFound)
	}
	if err := a.limits.demo.Allow(mw.ClientKey(mw.FromContext(ctx).ClientIP)); err != nil {
		return nil, err
	}
	u, token, err := a.auth.StartDemo(ctx, authFrom(ctx).client)
	if err != nil {
		return nil, err
	}
	if _, err := a.ledger.SeedDemo(ctx, u.ID); err != nil {
		return nil, errors.Join(err, a.auth.DiscardDemo(context.WithoutCancel(ctx), u.ID))
	}
	a.reclassifyAfter(ctx, u.ID)
	a.audit(ctx, u.ID, ledger.AuditLogin, "", map[string]any{"demo": true})
	return gen.StartDemo200JSONResponse{Body: a.userDTO(ctx, u), Headers: gen.StartDemo200ResponseHeaders{SetCookie: a.cookies.session(token)}}, nil
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

// assistantFor returns the limiter for a rate of questions per hour (the shared one at the
// server's rate).
func (l limits) assistantFor(perHour int64) *mw.Limiter {
	if perHour <= 0 || perHour >= l.aiPerHour || l.assistantMu == nil {
		return l.assistant
	}
	l.assistantMu.Lock()
	defer l.assistantMu.Unlock()
	lim, ok := l.assistantRates[perHour]
	if !ok {
		lim = mw.NewLimiter(mw.Policy{Events: int(perHour), Per: time.Hour}, l.clock, 0)
		l.assistantRates[perHour] = lim
	}
	return lim
}

// confirmPassword asks for the signed-in user's password again before an action that outlives or
// outranks the session: creating an access token, changing an administrator's role (SYS-4). Off
// with FINUANCE_CONFIRM_PASSWORD=false. A demo visitor has no password they know and is not asked.
func (a *api) confirmPassword(ctx context.Context, password *string) error {
	id := identity(ctx)
	if !a.cfg.ConfirmPassword || id.User.IsDemo {
		return nil
	}
	if err := a.limits.password.Allow(id.User.ID.String()); err != nil {
		return err
	}
	if password == nil || *password == "" {
		ve := &platform.ValidationError{}
		ve.Add("password", "is required: type your current password to confirm")
		return ve
	}
	return a.auth.CheckPassword(ctx, id.User.ID, "password", *password)
}
