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
	password     *mw.Limiter // password checks per signed-in user
	upload       *mw.Limiter // uploads per user
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
}

func newLimits(clock platform.Clock, aiPerHour int) limits {
	if aiPerHour < 1 {
		aiPerHour = 30
	}
	return limits{
		loginIP:        mw.NewLimiter(mw.LoginPerIP, clock, 0),
		loginIPEmail:   mw.NewLimiter(mw.LoginPerIPEmail, clock, 0),
		password:       mw.NewLimiter(mw.PasswordPerUser, clock, 0),
		upload:         mw.NewLimiter(mw.UploadPerUser, clock, 0),
		assistant:      mw.NewLimiter(mw.Policy{Events: aiPerHour, Per: time.Hour}, clock, 0),
		assistantMu:    &sync.Mutex{},
		assistantRates: map[int64]*mw.Limiter{},
		clock:          clock,
		aiPerHour:      int64(aiPerHour),
		demo:           mw.NewLimiter(mw.DemoPerIP, clock, 0),
		friendUser:     mw.NewLimiter(mw.FriendCodesPerHour, clock, 0),
		friendIP:       mw.NewLimiter(mw.FriendCodesPerHour, clock, 0),
		// A full bucket and one token back per lifetime + 1 h: exactly DemoAssistantMessages
		// before the user is deleted.
		demoAsk: mw.NewLimiter(mw.Policy{Events: config.DemoAssistantMessages,
			Per: config.DemoAssistantMessages * (auth.DemoLifetime + time.Hour)}, clock, 0),
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

func (a *api) userDTO(ctx context.Context, u auth.User) gen.User {
	l := a.effectiveLimits(ctx, u)
	return gen.User{Id: u.ID, Email: u.Email, DisplayName: u.DisplayName, IsDemo: u.IsDemo, CreatedAt: u.CreatedAt,
		IsAdmin: u.IsAdmin, MustChangePassword: u.MustChangePassword,
		Limits: gen.EffectiveLimits{MaxUploadBytes: l.MaxUploadBytes, ImportsPerDay: l.ImportsPerDay, MaxAccounts: l.MaxAccounts,
			MaxGroups: l.MaxGroups, AssistantPerHour: l.AssistantPerHour}}
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
	return gen.Login200JSONResponse{Body: a.userDTO(ctx, u), Headers: gen.Login200ResponseHeaders{SetCookie: a.cookies.session(token)}}, nil
}

// StartDemo signs a visitor in as a new demo user with the demo year imported (PLAN 19.1). Only
// on a demo instance; elsewhere the route doesn't exist.
func (a *api) StartDemo(ctx context.Context, _ gen.StartDemoRequestObject) (gen.StartDemoResponseObject, error) {
	if !a.cfg.DemoMode {
		return nil, fmt.Errorf("/auth/demo: %w", platform.ErrNotFound)
	}
	if err := a.limits.demo.Allow(mw.FromContext(ctx).ClientIP); err != nil {
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
