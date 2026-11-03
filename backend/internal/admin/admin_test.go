package admin_test

import (
	"context"
	"encoding/json"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/admin"
	"github.com/x2db736ac85h/finuance/backend/internal/auth"
	"github.com/x2db736ac85h/finuance/backend/internal/instance"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/testutil"
)

var testDB *testutil.DB

func TestMain(m *testing.M) { testutil.Main(m, &testDB) }

const pw = "correct horse battery"

type env struct {
	adm   *admin.Service
	auth  *auth.Service
	led   *ledger.Ledger
	clock *platform.FixedClock
}

func setup(t *testing.T) env {
	t.Helper()
	tdb := testutil.Require(t, testDB)
	tdb.Truncate(t)
	st := store.New(tdb.App)
	clk := &platform.FixedClock{T: time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)}
	a := auth.New(st, clk, auth.Config{Params: auth.Params{MemoryKiB: 8 * 1024, Time: 1, Threads: 1},
		SessionTTL: 24 * time.Hour, SessionIdleTTL: 24 * time.Hour, AllowSignup: true})
	return env{adm: admin.New(st, a, clk, instance.Bounds{MaxUploadBytes: 20 << 20, AssistantPerHour: 30}), auth: a,
		led: ledger.New(st, clk), clock: clk}
}

func (e env) user(t *testing.T, mail string, isAdmin bool) auth.User {
	t.Helper()
	u, err := e.auth.CreateUser(context.Background(), auth.SignupInput{Email: mail, Password: pw, Admin: isAdmin})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestUsageIsCountsOnly(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	boss := e.user(t, "boss@example.test", true)
	alex := e.user(t, "alex@example.test", false)
	seed, err := e.led.SeedDemo(ctx, alex.ID)
	if err != nil {
		t.Fatal(err)
	}
	u, err := e.adm.GetUser(ctx, alex.ID)
	if err != nil {
		t.Fatal(err)
	}
	if u.Usage.Accounts != 1 || u.Usage.Transactions != int64(seed.Result.New) || u.Usage.Imports != 1 || u.Usage.StoredBytes == 0 {
		t.Errorf("usage = %+v, seeded %d transactions", u.Usage, seed.Result.New)
	}
	page, err := e.adm.ListUsers(ctx, "", "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Users) != 2 || page.Totals.Users != 2 || page.Totals.Admins != 1 {
		t.Fatalf("page = %+v", page)
	}
	found, err := e.adm.ListUsers(ctx, "ALEX", "", 50)
	if err != nil || len(found.Users) != 1 || found.Users[0].ID != alex.ID {
		t.Fatalf("search = %+v, %v", found, err)
	}
	inst, err := e.adm.InstanceUsage(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if inst.Transactions != int64(seed.Result.New) || len(inst.Days) != 7 || inst.Days[6].Imports != 1 {
		t.Errorf("instance usage = %+v", inst)
	}
	_ = boss
}

func TestPaging(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	for i := range 5 {
		e.user(t, "u"+strconv.Itoa(i)+"@example.test", false)
		e.clock.T = e.clock.T.Add(time.Minute)
	}
	var seen []string
	cursor := ""
	for {
		page, err := e.adm.ListUsers(ctx, "", cursor, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, u := range page.Users {
			seen = append(seen, u.Email)
		}
		if page.Next == "" {
			break
		}
		cursor = page.Next
	}
	if len(seen) != 5 {
		t.Fatalf("paged through %v", seen)
	}
	if _, err := e.adm.ListUsers(ctx, "", "nonsense", 2); err == nil {
		t.Fatal("a bad cursor was accepted")
	}
}

func TestLastAdmin(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	boss := e.user(t, "boss@example.test", true)
	other := e.user(t, "other@example.test", false)
	f := false
	tr := true
	if _, err := e.adm.UpdateUser(ctx, other.ID, boss.ID, admin.UpdateInput{Admin: &f}); !errors.Is(err, platform.ErrLastAdmin) {
		t.Fatalf("demoting the last admin: %v", err)
	}
	if _, err := e.adm.UpdateUser(ctx, other.ID, boss.ID, admin.UpdateInput{Disabled: &tr}); !errors.Is(err, platform.ErrLastAdmin) {
		t.Fatalf("disabling the last admin: %v", err)
	}
	if _, err := e.adm.SetAdminByEmail(ctx, boss.Email, false); !errors.Is(err, platform.ErrLastAdmin) {
		t.Fatalf("revoke-admin on the last admin: %v", err)
	}
	if err := e.auth.DeleteAccount(ctx, auth.Identity{User: boss}, pw); !errors.Is(err, platform.ErrLastAdmin) {
		t.Fatalf("the last admin deleting their account: %v", err)
	}
	if _, err := e.adm.SetAdminByEmail(ctx, other.Email, true); err != nil {
		t.Fatal(err)
	}
	if u, err := e.adm.UpdateUser(ctx, other.ID, boss.ID, admin.UpdateInput{Admin: &f}); err != nil || u.IsAdmin {
		t.Fatalf("with two admins, one can be demoted: %+v, %v", u, err)
	}
	if _, err := e.adm.UpdateUser(ctx, other.ID, other.ID, admin.UpdateInput{Disabled: &tr}); err == nil {
		t.Fatal("an admin disabled themselves")
	}
	if _, err := e.adm.DeleteUser(ctx, other.ID, other.ID, other.Email); err == nil {
		t.Fatal("an admin deleted themselves through the panel")
	}
}

func TestDisableResetAndDelete(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	boss := e.user(t, "boss@example.test", true)
	alex := e.user(t, "alex@example.test", false)
	_, tok, err := e.auth.Login(ctx, alex.Email, pw, "", auth.Client{})
	if err != nil {
		t.Fatal(err)
	}
	tr, f := true, false
	if _, err := e.adm.UpdateUser(ctx, boss.ID, alex.ID, admin.UpdateInput{Disabled: &tr}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.auth.Resolve(ctx, tok); !errors.Is(err, platform.ErrUnauthenticated) {
		t.Fatalf("a disabled account's session still works: %v", err)
	}
	if _, _, err := e.auth.Login(ctx, alex.Email, pw, "", auth.Client{}); !errors.Is(err, platform.ErrAccountDisabled) {
		t.Fatalf("a disabled account signed in: %v", err)
	}
	if _, _, err := e.auth.Login(ctx, alex.Email, "wrong password!!", "", auth.Client{}); errors.Is(err, platform.ErrAccountDisabled) {
		t.Fatal("a wrong password tells that the account is disabled")
	}
	if _, err := e.adm.UpdateUser(ctx, boss.ID, alex.ID, admin.UpdateInput{Disabled: &f}); err != nil {
		t.Fatal(err)
	}
	one, err := e.adm.ResetPassword(ctx, alex.ID)
	if err != nil || len(one) != 19 {
		t.Fatalf("one-time password %q, %v", one, err)
	}
	u, tok, err := e.auth.Login(ctx, alex.Email, one, "", auth.Client{})
	if err != nil || !u.MustChangePassword {
		t.Fatalf("login with the one-time password: %+v, %v", u, err)
	}
	id, err := e.auth.Resolve(ctx, tok)
	if err != nil || !id.User.MustChangePassword {
		t.Fatalf("resolve: %+v, %v", id, err)
	}
	if _, err := e.auth.ChangePassword(ctx, id, one, one, auth.Client{}); err == nil {
		t.Fatal("the one-time password was kept as the new password")
	}
	tok, err = e.auth.ChangePassword(ctx, id, one, "a brand new password", auth.Client{})
	if err != nil {
		t.Fatal(err)
	}
	if id, err = e.auth.Resolve(ctx, tok); err != nil || id.User.MustChangePassword {
		t.Fatalf("after the change: %+v, %v", id, err)
	}
	if n, err := e.adm.RevokeSessions(ctx, alex.ID); err != nil || n != 1 {
		t.Fatalf("revoke = %d, %v", n, err)
	}
	if _, err := e.adm.DeleteUser(ctx, boss.ID, alex.ID, "someone@example.test"); err == nil {
		t.Fatal("deleted without the right confirmation")
	}
	if _, err := e.adm.DeleteUser(ctx, boss.ID, alex.ID, " ALEX@example.test "); err != nil {
		t.Fatal(err)
	}
	if _, err := e.adm.GetUser(ctx, alex.ID); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("deleted user still there: %v", err)
	}
}

func TestCreateWithOneTimePassword(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	u, one, err := e.adm.CreateUser(ctx, admin.CreateInput{Email: "new@example.test", DisplayName: "New"})
	if err != nil || one == "" || !u.MustChangePassword || u.IsAdmin {
		t.Fatalf("created %+v with %q: %v", u, one, err)
	}
	u2, one2, err := e.adm.CreateUser(ctx, admin.CreateInput{Email: "set@example.test", Password: pw, Admin: true})
	if err != nil || one2 != "" || u2.MustChangePassword || !u2.IsAdmin {
		t.Fatalf("created %+v with %q: %v", u2, one2, err)
	}
	if _, _, err := e.adm.CreateUser(ctx, admin.CreateInput{Email: "new@example.test"}); !errors.Is(err, platform.ErrConflict) {
		t.Fatalf("duplicate e-mail: %v", err)
	}
	if a, b := admin.OneTimePassword(), admin.OneTimePassword(); a == b || !strings.Contains(a, "-") {
		t.Fatalf("one-time passwords %q, %q", a, b)
	}
}

func TestLimitsAndSettings(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	boss := e.user(t, "boss@example.test", true)
	alex := e.user(t, "alex@example.test", false)
	three := int64(3)
	if _, err := e.adm.UpdateUser(ctx, boss.ID, alex.ID, admin.UpdateInput{Limits: &instance.Limits{MaxAccounts: &three}}); err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	if _, err := e.adm.UpdateUser(ctx, boss.ID, alex.ID, admin.UpdateInput{Limits: &instance.Limits{MaxAccounts: &zero}}); err == nil {
		t.Fatal("a zero limit was accepted")
	}
	_, v, err := e.adm.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	doc := json.RawMessage(`{"features":{"overview":false},"limits":{"maxAccounts":1,"maxUploadBytes":1048576},"defaults":{"themeId":"fjord"}}`)
	if _, _, err := e.adm.PutSettings(ctx, boss.ID, doc, v+5); !errors.Is(err, platform.ErrStaleVersion) {
		t.Fatalf("stale version: %v", err)
	}
	if _, _, err := e.adm.PutSettings(ctx, boss.ID, json.RawMessage(`{"defaults":{"themeId":"not-there"}}`), v); err == nil {
		t.Fatal("an unknown default theme was accepted")
	}
	if _, nv, err := e.adm.PutSettings(ctx, boss.ID, doc, v); err != nil || nv != v+1 {
		t.Fatalf("put: %d, %v", nv, err)
	}
	f, err := e.adm.Features(ctx)
	if err != nil || f.Overview || !f.Assistant {
		t.Fatalf("features %+v, %v", f, err)
	}
	u, err := e.auth.UserByEmail(ctx, alex.Email)
	if err != nil {
		t.Fatal(err)
	}
	l, err := e.adm.Limits(ctx, u)
	if err != nil || l.MaxAccounts != 3 || l.MaxUploadBytes != 1<<20 || l.AssistantPerHour != 30 {
		t.Fatalf("alex's limits %+v, %v", l, err)
	}
	pub, err := e.adm.Public(ctx)
	if err != nil || pub.Defaults.ThemeID == nil || *pub.Defaults.ThemeID != "fjord" {
		t.Fatalf("public %+v, %v", pub, err)
	}
}

func tokens(accentDark string) json.RawMessage {
	m := map[string]map[string]string{}
	for _, mode := range instance.ThemeModes {
		bg, fg := "#ffffff", "#000000"
		if mode == "dark" {
			bg, fg = "#000000", "#ffffff"
		}
		mm := map[string]string{}
		for _, k := range instance.ThemeKeys {
			mm[k] = bg
		}
		for _, k := range []string{"text", "muted", "accent", "pos", "neg", "on-accent"} {
			mm[k] = fg
		}
		mm["accent-solid"], mm["sel"], mm["accent-soft"] = bg, fg+"10", fg+"10"
		if mode == "dark" {
			mm["accent"] = accentDark
		}
		m[mode] = mm
	}
	raw, _ := json.Marshal(m)
	return raw
}

func TestThemes(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	boss := e.user(t, "boss@example.test", true)
	if _, err := e.adm.CreateTheme(ctx, boss.ID, admin.ThemeInput{Slug: "indigo", Name: "X", BasedOn: "indigo", Tokens: tokens("#ffffff")}); err == nil {
		t.Fatal("a built-in slug was accepted")
	}
	th, err := e.adm.CreateTheme(ctx, boss.ID, admin.ThemeInput{Slug: "sea", Name: "Sea", BasedOn: "fjord", Tokens: tokens("#222222")})
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.adm.SetThemeStatus(ctx, th.ID, th.Version, "approved")
	var ce *instance.ContrastError
	if !errors.As(err, &ce) || len(ce.Failures) == 0 || ce.Failures[0].FG != "accent" {
		t.Fatalf("approving a low-contrast theme: %v", err)
	}
	th, err = e.adm.UpdateTheme(ctx, th.ID, th.Version, "Sea", tokens("#ffffff"))
	if err != nil {
		t.Fatal(err)
	}
	if th, err = e.adm.SetThemeStatus(ctx, th.ID, th.Version, "approved"); err != nil || th.Status != "approved" {
		t.Fatalf("approve: %+v, %v", th, err)
	}
	if _, err := e.adm.UpdateTheme(ctx, th.ID, th.Version, "Sea", tokens("#222222")); !errors.As(err, &ce) {
		t.Fatalf("an approved theme took colours that fail contrast: %v", err)
	}
	pub, err := e.adm.Public(ctx)
	if err != nil || len(pub.Themes) != 1 || pub.Themes[0].Slug != "sea" {
		t.Fatalf("public themes %+v, %v", pub.Themes, err)
	}
	if err := e.adm.DeleteTheme(ctx, th.ID); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("deleted an approved theme: %v", err)
	}
	if _, err := e.adm.SetThemeStatus(ctx, th.ID, th.Version-1, "retired"); !errors.Is(err, platform.ErrStaleVersion) {
		t.Fatalf("stale retire: %v", err)
	}
}

// TestAdminImportsNoFinancialPackage keeps the administration away from financial data by
// construction: the package can't call the code that reads it (ADR 0025).
func TestAdminImportsNoFinancialPackage(t *testing.T) {
	banned := []string{"internal/ledger", "internal/query", "internal/insights", "internal/rules", "internal/assistant",
		"internal/legacyimport", "internal/investments", "internal/ingest"}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, en := range entries {
		name := en.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(".", name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			for _, b := range banned {
				if strings.HasSuffix(path, b) {
					t.Errorf("%s imports %s", name, path)
				}
			}
		}
	}
	if checked < 3 {
		t.Fatalf("checked only %d files", checked)
	}
}
