package actions

import (
	"context"
	"net/http"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/client/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/sim"
)

// The account: preferences, notifications, a data export (rare), and two deliberate mistakes: a
// mistyped password and an unknown path.
func account() []sim.Action {
	return []sim.Action{
		{Name: "account.preferences", Area: "account", Run: accountPreferences},
		{Name: "account.notifications", Area: "account", Run: accountNotifications},
		{Name: "account.export", Area: "account", Cost: sim.Heavy, Needs: sim.NeedData, Run: accountExport},
		{Name: "account.wrong-password", Area: "account", Mistake: true, Run: wrongPassword},
		{Name: "account.unknown-path", Area: "account", Mistake: true, Run: unknownPath},
	}
}

// accountPreferences reads the preferences document and saves it back, as the web app does after
// a change of view.
func accountPreferences(ctx context.Context, a *sim.Actor) error {
	got, err := a.API.GetPreferencesWithResponse(ctx)
	if err != nil {
		return err
	}
	if err := sim.Expect("preferences", got.HTTPResponse, http.StatusOK); err != nil {
		return err
	}
	put, err := a.API.PutPreferencesWithResponse(ctx, gen.PutPreferencesJSONRequestBody(*got.JSON200))
	if err != nil {
		return err
	}
	return sim.Expect("save preferences", put.HTTPResponse, http.StatusOK)
}

func accountNotifications(ctx context.Context, a *sim.Actor) error {
	list, err := a.API.ListNotificationsWithResponse(ctx, &gen.ListNotificationsParams{Limit: ptr(50)})
	if err != nil {
		return err
	}
	if err := sim.Expect("notifications", list.HTTPResponse, http.StatusOK); err != nil {
		return err
	}
	read, err := a.API.MarkNotificationsReadWithResponse(ctx, gen.MarkNotificationsReadJSONRequestBody{})
	if err != nil {
		return err
	}
	return sim.Expect("mark read", read.HTTPResponse, http.StatusNoContent)
}

// accountExport downloads the data export; once a day at most per persona.
func accountExport(ctx context.Context, a *sim.Actor) error {
	if !a.Every("account.export", 24*time.Hour) {
		return sim.ErrNothingToDo
	}
	r, err := a.API.ExportMeWithResponse(ctx)
	if err != nil {
		return err
	}
	return sim.Expect("data export", r.HTTPResponse, http.StatusOK)
}

// wrongPassword is a mistyped password: 401. At most one every ten minutes per persona, so that
// the instance's limit of failures per e-mail address never locks the persona out, and only on a
// sign-in turn: it counts against the instance's 30 sign-ins a minute per address, which every
// persona shares (in load mode they ran past it, found in the first load run).
func wrongPassword(ctx context.Context, a *sim.Actor) error {
	if !a.Every("account.wrong-password", 10*time.Minute) {
		return sim.ErrNothingToDo
	}
	if err := a.Engine().SignInTurn(ctx); err != nil {
		return err
	}
	r, err := a.Anon.LoginWithResponse(ctx, gen.LoginJSONRequestBody{Email: a.Persona.Email, Password: a.Password() + "x"})
	if err != nil {
		return err
	}
	return sim.Expect("a mistyped password", r.HTTPResponse, http.StatusUnauthorized)
}

// unknownPath is a request for a path no route has: 404. (A path that matches a route with a
// parameter in the wrong form, /transactions/latest, is a 422 the router gives before the session
// layer reads the token: such a request carries no user and counts as real, as signing in does.)
func unknownPath(ctx context.Context, a *sim.Actor) error {
	r, err := a.Raw(ctx, http.MethodGet, "/"+sim.Pick(a, "budget", "reports", "export.csv", "v2/accounts"))
	if err != nil {
		return err
	}
	defer func() { _ = r.Body.Close() }()
	return sim.Expect("an unknown path", r, http.StatusNotFound)
}
