package httpapi

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/friends"
	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/gen"
	mw "github.com/x2db736ac85h/finuance/backend/internal/httpapi/middleware"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// Friends (Phase 19, ADR 0027) and in-app notifications (19.8). The friends routes answer 404
// when the instance switched friends off (session gate, featureRoutes), in demo mode and on a
// bare server.

func (a *api) friendsOn() error {
	if a.friends == nil || a.cfg.DemoMode {
		return fmt.Errorf("friends: %w", platform.ErrNotFound)
	}
	return nil
}

func friendDTO(f friends.Friend) gen.Friend {
	return gen.Friend{Id: f.ID, DisplayName: f.DisplayName, Since: f.Since}
}

// ListFriends returns the user's friends.
func (a *api) ListFriends(ctx context.Context, _ gen.ListFriendsRequestObject) (gen.ListFriendsResponseObject, error) {
	if err := a.friendsOn(); err != nil {
		return nil, err
	}
	list, err := a.friends.List(ctx, identity(ctx).User.ID)
	if err != nil {
		return nil, err
	}
	out := make(gen.ListFriends200JSONResponse, len(list))
	for i, f := range list {
		out[i] = friendDTO(f)
	}
	return out, nil
}

// NewFriendCode makes the user's code.
func (a *api) NewFriendCode(ctx context.Context, _ gen.NewFriendCodeRequestObject) (gen.NewFriendCodeResponseObject, error) {
	if err := a.friendsOn(); err != nil {
		return nil, err
	}
	c, err := a.friends.NewCode(ctx, identity(ctx).User.ID)
	if err != nil {
		return nil, err
	}
	return gen.NewFriendCode201JSONResponse{Code: c.Code, ExpiresAt: c.ExpiresAt}, nil
}

// AddFriend enters someone's code. Every try counts against both limits; a failed one is audited.
func (a *api) AddFriend(ctx context.Context, req gen.AddFriendRequestObject) (gen.AddFriendResponseObject, error) {
	if err := a.friendsOn(); err != nil {
		return nil, err
	}
	u := identity(ctx).User.ID
	if err := a.limits.friendUser.Allow(u.String()); err != nil {
		return nil, err
	}
	if err := a.limits.friendIP.Allow(mw.FromContext(ctx).ClientIP); err != nil {
		return nil, err
	}
	f, err := a.friends.Redeem(ctx, u, req.Body.Code)
	if errors.Is(err, friends.ErrInvalid) {
		a.audit(ctx, u, ledger.AuditFriendCodeFailed, "", nil)
	}
	if err != nil {
		return nil, err
	}
	a.audit(ctx, u, ledger.AuditFriendAdded, f.ID.String(), map[string]any{"name": f.DisplayName})
	a.audit(ctx, f.ID, ledger.AuditFriendAdded, u.String(), map[string]any{"by": "friend", "name": identity(ctx).User.DisplayName})
	return gen.AddFriend201JSONResponse(friendDTO(f)), nil
}

// RemoveFriend ends a friendship.
func (a *api) RemoveFriend(ctx context.Context, req gen.RemoveFriendRequestObject) (gen.RemoveFriendResponseObject, error) {
	if err := a.friendsOn(); err != nil {
		return nil, err
	}
	u := identity(ctx).User.ID
	if err := a.friends.Remove(ctx, u, req.Id); err != nil {
		return nil, err
	}
	a.audit(ctx, u, ledger.AuditFriendRemoved, req.Id.String(), nil)
	a.audit(ctx, req.Id, ledger.AuditFriendRemoved, u.String(), map[string]any{"by": "friend"})
	return gen.RemoveFriend204Response{}, nil
}

// ListNotifications returns the newest notifications and the unread count.
func (a *api) ListNotifications(ctx context.Context, req gen.ListNotificationsRequestObject) (gen.ListNotificationsResponseObject, error) {
	out := gen.ListNotifications200JSONResponse{Items: []gen.Notification{}}
	if a.notify == nil {
		return out, nil
	}
	limit := 30
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	list, unread, err := a.notify.List(ctx, identity(ctx).User.ID, limit)
	if err != nil {
		return nil, err
	}
	out.Unread = unread
	for _, n := range list {
		out.Items = append(out.Items, gen.Notification{Id: n.ID, Kind: gen.NotificationKind(n.Kind), Data: n.Data, CreatedAt: n.CreatedAt, ReadAt: n.ReadAt})
	}
	return out, nil
}

// MarkNotificationsRead marks notifications read.
func (a *api) MarkNotificationsRead(ctx context.Context, req gen.MarkNotificationsReadRequestObject) (gen.MarkNotificationsReadResponseObject, error) {
	if a.notify != nil {
		var ids []uuid.UUID
		if req.Body.Ids != nil {
			ids = *req.Body.Ids
		}
		if _, err := a.notify.MarkRead(ctx, identity(ctx).User.ID, ids); err != nil {
			return nil, err
		}
	}
	return gen.MarkNotificationsRead204Response{}, nil
}
