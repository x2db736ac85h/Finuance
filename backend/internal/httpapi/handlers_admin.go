package httpapi

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/x2db736ac85h/finuance/backend/internal/admin"
	"github.com/x2db736ac85h/finuance/backend/internal/auth"
	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/instance"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
)

// The admin routes (ADR 0025). requireSession answers 404 for them unless the signed-in user is an
// administrator, so the handlers below only run for administrators. Every change is recorded in
// the administrator's Activity and, without naming them, in the affected user's.

func (a *api) adminAudit(ctx context.Context, action string, target *admin.User, meta map[string]any) {
	if meta == nil {
		meta = map[string]any{}
	}
	if target != nil {
		meta["email"] = target.Email
		entity := target.ID.String()
		a.auditMe(ctx, action, entity, meta)
		if target.ID != identity(ctx).User.ID {
			// the user learns that an administrator acted, not which one
			a.audit(ctx, target.ID, action, entity, map[string]any{"by": "administrator"})
		}
		return
	}
	a.auditMe(ctx, action, "", meta)
}

func limitsDTO(l instance.Limits) gen.LimitOverrides {
	return gen.LimitOverrides{MaxUploadBytes: l.MaxUploadBytes, ImportsPerDay: l.ImportsPerDay, MaxAccounts: l.MaxAccounts,
		MaxGroups: l.MaxGroups, MaxSharedGroups: l.MaxSharedGroups, AssistantPerHour: l.AssistantPerHour}
}

func limitsIn(l gen.LimitOverrides) instance.Limits {
	return instance.Limits{MaxUploadBytes: l.MaxUploadBytes, ImportsPerDay: l.ImportsPerDay, MaxAccounts: l.MaxAccounts,
		MaxGroups: l.MaxGroups, MaxSharedGroups: l.MaxSharedGroups, AssistantPerHour: l.AssistantPerHour}
}

func adminUserDTO(u admin.User) gen.AdminUser {
	return gen.AdminUser{Id: u.ID, Email: u.Email, DisplayName: u.DisplayName, IsDemo: u.IsDemo, IsAdmin: u.IsAdmin,
		Disabled: u.Disabled, MustChangePassword: u.MustChangePassword, CreatedAt: u.CreatedAt, LastLoginAt: u.LastLoginAt,
		Limits: limitsDTO(u.Limits), Usage: gen.UserUsage{Accounts: u.Usage.Accounts, Transactions: u.Usage.Transactions,
			Imports: u.Usage.Imports, StoredBytes: u.Usage.StoredBytes, Chats: u.Usage.Chats, LastActivity: u.Usage.LastActivity}}
}

func totalsDTO(t admin.Totals) gen.UserTotals {
	return gen.UserTotals{Users: t.Users, Admins: t.Admins, Disabled: t.Disabled, Demo: t.Demo, Active7: t.Active7, Active30: t.Active30}
}

// AdminListUsers returns one page of accounts with their usage counts.
func (a *api) AdminListUsers(ctx context.Context, req gen.AdminListUsersRequestObject) (gen.AdminListUsersResponseObject, error) {
	var search, cursor string
	var limit int32 = 50
	if req.Params.Search != nil {
		search = *req.Params.Search
	}
	if req.Params.Cursor != nil {
		cursor = *req.Params.Cursor
	}
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	page, err := a.admin.ListUsers(ctx, search, cursor, limit)
	if err != nil {
		return nil, err
	}
	out := gen.AdminUsersPage{Users: make([]gen.AdminUser, 0, len(page.Users)), Totals: totalsDTO(page.Totals)}
	for i := range page.Users {
		out.Users = append(out.Users, adminUserDTO(page.Users[i]))
	}
	if page.Next != "" {
		out.Next = &page.Next
	}
	return gen.AdminListUsers200JSONResponse(out), nil
}

// AdminCreateUser creates an account (with a one-time password when none is sent).
func (a *api) AdminCreateUser(ctx context.Context, req gen.AdminCreateUserRequestObject) (gen.AdminCreateUserResponseObject, error) {
	b := req.Body
	in := admin.CreateInput{Email: b.Email, DisplayName: deref(b.DisplayName), Password: deref(b.Password), Admin: b.Admin != nil && *b.Admin}
	u, oneTime, err := a.admin.CreateUser(ctx, in)
	if err != nil {
		return nil, err
	}
	a.adminAudit(ctx, ledger.AuditAdminUserCreated, &u, map[string]any{"admin": u.IsAdmin})
	out := gen.AdminCreatedUser{User: adminUserDTO(u)}
	if oneTime != "" {
		out.OneTimePassword = &oneTime
	}
	return gen.AdminCreateUser201JSONResponse(out), nil
}

// AdminGetUser returns one account.
func (a *api) AdminGetUser(ctx context.Context, req gen.AdminGetUserRequestObject) (gen.AdminGetUserResponseObject, error) {
	u, err := a.admin.GetUser(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return gen.AdminGetUser200JSONResponse(adminUserDTO(u)), nil
}

// AdminUpdateUser changes the role, state, display name or limits of an account.
func (a *api) AdminUpdateUser(ctx context.Context, req gen.AdminUpdateUserRequestObject) (gen.AdminUpdateUserResponseObject, error) {
	b := req.Body
	before, err := a.admin.GetUser(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	in := admin.UpdateInput{Admin: b.Admin, Disabled: b.Disabled, DisplayName: b.DisplayName}
	if b.Limits != nil {
		l := limitsIn(*b.Limits)
		in.Limits = &l
	}
	u, err := a.admin.UpdateUser(ctx, identity(ctx).User.ID, req.Id, in)
	if err != nil {
		return nil, err
	}
	if b.Admin != nil && *b.Admin != before.IsAdmin {
		a.adminAudit(ctx, ledger.AuditAdminRoleChanged, &u, map[string]any{"admin": u.IsAdmin})
	}
	if b.Disabled != nil && *b.Disabled != before.Disabled {
		action := ledger.AuditAdminUserEnabled
		if u.Disabled {
			action = ledger.AuditAdminUserDisabled
		}
		a.adminAudit(ctx, action, &u, nil)
	}
	if b.Limits != nil {
		a.adminAudit(ctx, ledger.AuditAdminLimitsChanged, &u, nil)
	}
	return gen.AdminUpdateUser200JSONResponse(adminUserDTO(u)), nil
}

// AdminDeleteUser deletes an account and all of its data.
func (a *api) AdminDeleteUser(ctx context.Context, req gen.AdminDeleteUserRequestObject) (gen.AdminDeleteUserResponseObject, error) {
	gone, err := a.admin.DeleteUser(ctx, identity(ctx).User.ID, req.Id, req.Body.ConfirmEmail)
	if err != nil {
		return nil, err
	}
	a.auditMe(ctx, ledger.AuditAdminUserDeleted, gone.ID.String(), map[string]any{"email": gone.Email})
	return gen.AdminDeleteUser204Response{}, nil
}

// AdminResetPassword gives the account a new one-time password.
func (a *api) AdminResetPassword(ctx context.Context, req gen.AdminResetPasswordRequestObject) (gen.AdminResetPasswordResponseObject, error) {
	u, err := a.admin.GetUser(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	pw, err := a.admin.ResetPassword(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	a.adminAudit(ctx, ledger.AuditAdminPasswordReset, &u, nil)
	return gen.AdminResetPassword200JSONResponse{OneTimePassword: pw}, nil
}

// AdminRevokeSessions signs the account out everywhere.
func (a *api) AdminRevokeSessions(ctx context.Context, req gen.AdminRevokeSessionsRequestObject) (gen.AdminRevokeSessionsResponseObject, error) {
	u, err := a.admin.GetUser(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	n, err := a.admin.RevokeSessions(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	a.adminAudit(ctx, ledger.AuditAdminSessionsRevoked, &u, map[string]any{"sessions": n})
	return gen.AdminRevokeSessions200JSONResponse{Revoked: n}, nil
}

func instanceSettingsDTO(s instance.Settings, version int32) (gen.InstanceSettingsDoc, error) {
	var out gen.InstanceSettingsDoc
	raw, _ := json.Marshal(s) // the field names are the API's
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("settings document: %w", err)
	}
	out.Version = version
	return out, nil
}

// AdminGetSettings returns the instance document.
func (a *api) AdminGetSettings(ctx context.Context, _ gen.AdminGetSettingsRequestObject) (gen.AdminGetSettingsResponseObject, error) {
	s, v, err := a.admin.Settings(ctx)
	if err != nil {
		return nil, err
	}
	out, err := instanceSettingsDTO(s, v)
	return gen.AdminGetSettings200JSONResponse(out), err
}

// AdminPutSettings replaces the instance document (versioned).
func (a *api) AdminPutSettings(ctx context.Context, req gen.AdminPutSettingsRequestObject) (gen.AdminPutSettingsResponseObject, error) {
	body := *req.Body
	version := body.Version
	body.Version, body.UpdatedAt = 0, nil
	raw, _ := json.Marshal(struct {
		Defaults gen.ViewDefaults   `json:"defaults"`
		Features any                `json:"features"`
		Limits   gen.LimitOverrides `json:"limits"`
	}{body.Defaults, body.Features, body.Limits})
	s, v, err := a.admin.PutSettings(ctx, identity(ctx).User.ID, raw, version)
	if err != nil {
		return nil, err
	}
	a.adminAudit(ctx, ledger.AuditAdminSettingsChanged, nil, map[string]any{"version": v})
	out, err := instanceSettingsDTO(s, v)
	return gen.AdminPutSettings200JSONResponse(out), err
}

// AdminUsage returns the instance totals and recent activity.
func (a *api) AdminUsage(ctx context.Context, req gen.AdminUsageRequestObject) (gen.AdminUsageResponseObject, error) {
	days := 30
	if req.Params.Days != nil {
		days = *req.Params.Days
	}
	u, err := a.admin.InstanceUsage(ctx, days)
	if err != nil {
		return nil, err
	}
	out := gen.AdminUsage200JSONResponse{Totals: totalsDTO(u.Totals), Accounts: u.Accounts, Transactions: u.Transactions,
		Imports: u.Imports, StoredBytes: u.StoredBytes, Days: make([]gen.DayUsage, 0, len(u.Days))}
	for _, d := range u.Days {
		out.Days = append(out.Days, gen.DayUsage{Day: *dateDTO(&d.Day), Imports: d.Imports, Questions: d.Questions})
	}
	return out, nil
}

func themeTokensDTO(t instance.ThemeTokens) gen.ThemeTokens {
	return gen.ThemeTokens{Dark: gen.ThemeColours(t["dark"]), Light: gen.ThemeColours(t["light"])}
}

func themeDTO(t admin.Theme) gen.Theme {
	return gen.Theme{Id: t.ID, Slug: t.Slug, Name: t.Name, BasedOn: t.BasedOn, Tokens: themeTokensDTO(t.Tokens),
		Status: gen.ThemeStatus(t.Status), Version: t.Version, UpdatedAt: t.UpdatedAt}
}

// AdminListThemes lists the administrator-made themes.
func (a *api) AdminListThemes(ctx context.Context, _ gen.AdminListThemesRequestObject) (gen.AdminListThemesResponseObject, error) {
	themes, err := a.admin.Themes(ctx)
	if err != nil {
		return nil, err
	}
	out := make(gen.AdminListThemes200JSONResponse, 0, len(themes))
	for _, t := range themes {
		out = append(out, themeDTO(t))
	}
	return out, nil
}

// AdminCreateTheme stores a draft theme.
func (a *api) AdminCreateTheme(ctx context.Context, req gen.AdminCreateThemeRequestObject) (gen.AdminCreateThemeResponseObject, error) {
	raw, _ := json.Marshal(req.Body.Tokens)
	t, err := a.admin.CreateTheme(ctx, identity(ctx).User.ID, admin.ThemeInput{Slug: req.Body.Slug, Name: req.Body.Name,
		BasedOn: string(req.Body.BasedOn), Tokens: raw})
	if err != nil {
		return nil, err
	}
	return gen.AdminCreateTheme201JSONResponse(themeDTO(t)), nil
}

// AdminUpdateTheme changes a theme's name and colours.
func (a *api) AdminUpdateTheme(ctx context.Context, req gen.AdminUpdateThemeRequestObject) (gen.AdminUpdateThemeResponseObject, error) {
	raw, _ := json.Marshal(req.Body.Tokens)
	t, err := a.admin.UpdateTheme(ctx, req.Id, req.Body.Version, req.Body.Name, raw)
	if err != nil {
		return nil, err
	}
	return gen.AdminUpdateTheme200JSONResponse(themeDTO(t)), nil
}

// AdminDeleteTheme deletes a draft.
func (a *api) AdminDeleteTheme(ctx context.Context, req gen.AdminDeleteThemeRequestObject) (gen.AdminDeleteThemeResponseObject, error) {
	if err := a.admin.DeleteTheme(ctx, req.Id); err != nil {
		return nil, err
	}
	return gen.AdminDeleteTheme204Response{}, nil
}

// AdminSetThemeStatus approves, retires or reopens a theme.
func (a *api) AdminSetThemeStatus(ctx context.Context, req gen.AdminSetThemeStatusRequestObject) (gen.AdminSetThemeStatusResponseObject, error) {
	t, err := a.admin.SetThemeStatus(ctx, req.Id, req.Body.Version, string(req.Body.Status))
	if err != nil {
		return nil, err
	}
	switch t.Status {
	case "approved":
		a.adminAudit(ctx, ledger.AuditAdminThemeApproved, nil, map[string]any{"theme": t.Slug})
	case "retired":
		a.adminAudit(ctx, ledger.AuditAdminThemeRetired, nil, map[string]any{"theme": t.Slug})
	}
	return gen.AdminSetThemeStatus200JSONResponse(themeDTO(t)), nil
}

// features are the switches in effect; without an admin service (bare-server tests) all are on.
func (a *api) features(ctx context.Context) instance.EffectiveFeatures {
	if a.admin == nil {
		return instance.EffectiveFeatures{Overview: true, Assistant: true, Friends: true, SharedGroups: true}
	}
	f, err := a.admin.Features(ctx)
	if err != nil { // the settings are unreadable: keep the app usable, like a fresh instance
		return instance.EffectiveFeatures{Overview: true, Assistant: true, Friends: true, SharedGroups: true}
	}
	return f
}

// effectiveLimits are the signed-in user's limits.
func (a *api) effectiveLimits(ctx context.Context, u auth.User) instance.Effective {
	bounds := instance.Bounds{MaxUploadBytes: a.cfg.MaxUploadBytes, AssistantPerHour: int64(a.cfg.AIRatePerHour)}
	if a.admin == nil {
		return instance.Resolve(u.Limits, instance.Limits{}, bounds)
	}
	l, err := a.admin.Limits(ctx, u)
	if err != nil {
		return instance.Resolve(u.Limits, instance.Limits{}, bounds)
	}
	return l
}

// checkUpload enforces the signed-in user's upload size limit (the body limit is the server's).
func (a *api) checkUpload(ctx context.Context, size int64) error {
	if a.admin == nil {
		return nil
	}
	return a.admin.CheckUpload(ctx, identity(ctx).User, size)
}

// checkCount enforces a count limit (imports per day, accounts, groups) for the signed-in user.
func (a *api) checkCount(ctx context.Context, limit string) error {
	if a.admin == nil {
		return nil
	}
	return a.admin.CheckCount(ctx, identity(ctx).User, limit)
}
