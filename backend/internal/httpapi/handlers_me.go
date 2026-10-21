package httpapi

import (
	"context"
	"errors"

	"github.com/x2db736ac85h/finuance/backend/internal/auth"
	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/gen"
)

// GetMe returns the signed-in user.
func (a *api) GetMe(ctx context.Context, _ gen.GetMeRequestObject) (gen.GetMeResponseObject, error) {
	return gen.GetMe200JSONResponse(userDTO(identity(ctx).User)), nil
}

// UpdateMe changes the display name.
func (a *api) UpdateMe(ctx context.Context, req gen.UpdateMeRequestObject) (gen.UpdateMeResponseObject, error) {
	u, err := a.auth.UpdateDisplayName(ctx, identity(ctx), req.Body.DisplayName)
	if err != nil {
		return nil, err
	}
	return gen.UpdateMe200JSONResponse(userDTO(u)), nil
}

// ChangePassword checks the current password, revokes every session and signs this browser in
// again with a new session.
func (a *api) ChangePassword(ctx context.Context, req gen.ChangePasswordRequestObject) (gen.ChangePasswordResponseObject, error) {
	id := identity(ctx)
	if err := a.limits.password.Allow(id.User.ID.String()); err != nil {
		return nil, err
	}
	token, err := a.auth.ChangePassword(ctx, id, req.Body.CurrentPassword, req.Body.NewPassword, authFrom(ctx).client)
	if err != nil {
		return nil, err
	}
	return gen.ChangePassword204Response{Headers: gen.ChangePassword204ResponseHeaders{SetCookie: a.cookies.session(token)}}, nil
}

// DeleteMe deletes the account and all of its data after checking the password.
func (a *api) DeleteMe(ctx context.Context, req gen.DeleteMeRequestObject) (gen.DeleteMeResponseObject, error) {
	id := identity(ctx)
	if err := a.limits.password.Allow(id.User.ID.String()); err != nil {
		return nil, err
	}
	if err := a.auth.DeleteAccount(ctx, id, req.Body.Password); err != nil {
		return nil, err
	}
	return gen.DeleteMe204Response{Headers: gen.DeleteMe204ResponseHeaders{SetCookie: a.cookies.clear()}}, nil
}

// ListSessions lists the user's live sessions.
func (a *api) ListSessions(ctx context.Context, _ gen.ListSessionsRequestObject) (gen.ListSessionsResponseObject, error) {
	list, err := a.auth.Sessions(ctx, identity(ctx))
	if err != nil {
		return nil, err
	}
	out := make(gen.ListSessions200JSONResponse, len(list))
	for i := range list {
		s := &list[i]
		out[i] = gen.Session{Id: s.ID, CreatedAt: s.CreatedAt, LastSeenAt: s.LastSeenAt, ExpiresAt: s.ExpiresAt,
			UserAgent: s.UserAgent, Current: s.Current}
		if s.IP != "" {
			ip := s.IP
			out[i].Ip = &ip
		}
	}
	return out, nil
}

// RevokeSession signs out one of the user's sessions.
func (a *api) RevokeSession(ctx context.Context, req gen.RevokeSessionRequestObject) (gen.RevokeSessionResponseObject, error) {
	if err := a.auth.RevokeSession(ctx, identity(ctx), req.Id); err != nil {
		return nil, err
	}
	return gen.RevokeSession204Response{}, nil
}

// GetPreferences returns the UI preferences document.
func (a *api) GetPreferences(ctx context.Context, _ gen.GetPreferencesRequestObject) (gen.GetPreferencesResponseObject, error) {
	p, err := a.auth.GetPreferences(ctx, identity(ctx))
	if err != nil {
		return nil, err
	}
	return gen.GetPreferences200JSONResponse{Version: p.Version, Prefs: p.Prefs}, nil
}

// PutPreferences replaces the document if the client's version is current; otherwise it answers
// 409 stale_version with the current document.
func (a *api) PutPreferences(ctx context.Context, req gen.PutPreferencesRequestObject) (gen.PutPreferencesResponseObject, error) {
	v, err := a.auth.SavePreferences(ctx, identity(ctx), req.Body.Version, req.Body.Prefs)
	var stale *auth.StaleVersionError
	if errors.As(err, &stale) {
		p := ProblemFor(err, nil, a.prod)
		return gen.PutPreferences409ApplicationProblemPlusJSONResponse{Type: p.Type, Title: p.Title, Status: p.Status,
			Code: p.Code, Detail: p.Detail, RequestId: requestID(ctx), Instance: ptr("/api/v1/preferences"),
			Current: gen.Preferences{Version: stale.Current.Version, Prefs: stale.Current.Prefs}}, nil
	}
	if err != nil {
		return nil, err
	}
	return gen.PutPreferences200JSONResponse{Version: v, Prefs: req.Body.Prefs}, nil
}

func ptr[T any](v T) *T { return &v }
