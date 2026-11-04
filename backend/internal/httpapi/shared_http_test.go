package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/config"
	tu "github.com/x2db736ac85h/finuance/backend/testutil"
)

// befriendHTTP makes two signed-in users friends through a code.
func befriendHTTP(t *testing.T, a, b *browser) {
	t.Helper()
	code := a.do("POST", "/friends/code", nil)
	expect(t, code, 201, "")
	expect(t, b.do("POST", "/friends", map[string]any{"code": code.body["code"]}), 201, "")
}

func friendID(t *testing.T, b *browser, name string) string {
	t.Helper()
	for _, f := range list(t, b.do("GET", "/friends", nil)) {
		if f["displayName"] == name {
			return f["id"].(string)
		}
	}
	t.Fatalf("no friend %s", name)
	return ""
}

func memberID(t *testing.T, detail resp, name string) string {
	t.Helper()
	for _, m := range detail.body["members"].([]any) {
		if mm := m.(map[string]any); mm["displayName"] == name {
			return mm["id"].(string)
		}
	}
	t.Fatalf("no member %s in %s", name, detail.raw)
	return ""
}

func balanceOf(detail resp, member string) int {
	for _, b := range detail.body["balances"].([]any) {
		if bb := b.(map[string]any); bb["memberId"] == member {
			return num(bb["balanceMinor"])
		}
	}
	return -1 << 31
}

// TestSharedGroupsHTTP: a trip shared between friends end to end, with what each side may see and
// do (ADR 0028).
func TestSharedGroupsHTTP(t *testing.T) {
	e := newTestEnv(t, nil, false)
	alex, bo, out := named(t, e, "alex", "Alex"), named(t, e, "bo", "Bo"), named(t, e, "out", "Out")
	befriendHTTP(t, alex, bo)

	// a personal group with a typed cash expense, then shared
	wallet := alex.do("POST", "/accounts", map[string]any{"name": "Portfel", "kind": "cash"})
	expect(t, wallet, 201, "")
	grp := alex.do("POST", "/groups", map[string]any{"name": "Kraków"})
	expect(t, grp, 201, "")
	tx := alex.do("POST", "/transactions", map[string]any{"accountId": wallet.body["id"], "bookedOn": "2026-06-02",
		"amountMinor": -6000, "payee": "Obwarzanki", "groupId": grp.body["id"]})
	expect(t, tx, 201, "")
	g := alex.do("POST", "/shared-groups", map[string]any{"name": "Kraków", "kind": "trip", "fromGroupId": grp.body["id"]})
	expect(t, g, 201, "")
	gid := g.body["id"].(string)
	if g.body["linkedGroupId"] != nil && g.body["linkedGroupId"] != grp.body["id"] {
		t.Errorf("linked = %v", g.body["linkedGroupId"])
	}
	for _, pg := range groupList(t, alex) {
		if pg["id"] == grp.body["id"] && pg["sharedGroupId"] != gid {
			t.Errorf("personal group = %v", pg)
		}
	}
	expect(t, alex.do("POST", "/shared-groups", map[string]any{"name": "Again", "fromGroupId": grp.body["id"]}), 409, "conflict")

	// invites: friends only
	expect(t, alex.do("POST", "/shared-groups/"+gid+"/invites", map[string]any{"friendId": e.userID(t, out)}), 422, "validation_failed")
	inv := alex.do("POST", "/shared-groups/"+gid+"/invites", map[string]any{"friendId": friendID(t, alex, "Bo")})
	expect(t, inv, 201, "")
	if inv.body["state"] != "invited" || strings.Contains(inv.raw, "@") {
		t.Errorf("invite = %s", inv.raw)
	}
	// Bo sees the invitation, not the expenses; the outsider sees nothing
	bl := bo.do("GET", "/shared-groups", nil)
	expect(t, bl, 200, "")
	if !strings.Contains(bl.raw, `"invited"`) {
		t.Errorf("bo's list = %s", bl.raw)
	}
	bd := bo.do("GET", "/shared-groups/"+gid, nil)
	expect(t, bd, 200, "")
	if len(bd.body["expenses"].([]any)) != 0 || strings.Contains(bd.raw, "@") {
		t.Errorf("invited detail = %s", bd.raw)
	}
	expect(t, bo.do("GET", "/shared-groups/"+gid+"/report", nil), 409, "conflict")
	expect(t, out.do("GET", "/shared-groups/"+gid, nil), 404, "not_found")
	expect(t, out.do("POST", "/shared-groups/"+gid+"/accept", nil), 404, "not_found")
	expect(t, out.do("POST", "/shared-groups/"+gid+"/expenses", map[string]any{"bookedOn": "2026-06-02", "amountMinor": 100}), 404, "not_found")
	if n := bo.do("GET", "/notifications", nil); !strings.Contains(n.raw, `"shared.invited"`) {
		t.Errorf("bo's notifications = %s", n.raw)
	}

	acc := bo.do("POST", "/shared-groups/"+gid+"/accept", nil)
	expect(t, acc, 200, "")
	if acc.body["linkedGroupId"] == nil {
		t.Errorf("accept made no personal group: %s", acc.raw)
	}
	exp := bo.do("POST", "/shared-groups/"+gid+"/expenses", map[string]any{"bookedOn": "2026-06-03", "amountMinor": 3000,
		"payee": "Muzeum", "category": "Kultura"})
	expect(t, exp, 201, "")
	d := alex.do("GET", "/shared-groups/"+gid, nil)
	expect(t, d, 200, "")
	a, b := memberID(t, d, "Alex"), memberID(t, d, "Bo")
	// Alex paid 60 alone (shared before Bo joined, so split to Alex only); Bo paid 30 split equally
	if len(d.body["expenses"].([]any)) != 2 || balanceOf(d, a) != -1500 || balanceOf(d, b) != 1500 {
		t.Fatalf("detail = %s", d.raw)
	}
	if strings.Contains(d.raw, "Portfel") || strings.Contains(d.raw, wallet.body["id"].(string)) {
		t.Errorf("the account leaks into the group: %s", d.raw)
	}
	// Bo can't change Alex's copy
	var alexExp string
	for _, x := range d.body["expenses"].([]any) {
		if xx := x.(map[string]any); xx["payee"] == "Obwarzanki" {
			alexExp = xx["id"].(string)
		}
	}
	expect(t, bo.do("PATCH", "/shared-groups/"+gid+"/expenses/"+alexExp, map[string]any{"split": map[string]any{"mode": "equal"}}), 403, "forbidden")
	expect(t, alex.do("DELETE", "/shared-groups/"+gid+"/expenses/"+alexExp, nil), 409, "conflict")
	// re-split Alex's copy equally: now Bo owes Alex 30 − 15 … balances: Alex +30 −15 = +15? (paid 60, share 30+15)
	expect(t, alex.do("PATCH", "/shared-groups/"+gid+"/expenses/"+alexExp, map[string]any{"split": map[string]any{"mode": "equal"}}), 204, "")
	d = alex.do("GET", "/shared-groups/"+gid, nil)
	if balanceOf(d, a) != 1500 || balanceOf(d, b) != -1500 || len(d.body["transfers"].([]any)) != 1 {
		t.Fatalf("after the re-split: %s", d.raw)
	}
	// report, export, timeline
	rep := bo.do("GET", "/shared-groups/"+gid+"/report", nil)
	expect(t, rep, 200, "")
	if num(rep.body["totalMinor"]) != 9000 || num(rep.body["days"]) != 2 || len(rep.body["members"].([]any)) != 2 {
		t.Errorf("report = %s", rep.raw)
	}
	csv := bo.do("GET", "/shared-groups/"+gid+"/export", nil)
	expect(t, csv, 200, "")
	if !strings.HasPrefix(csv.raw, "\ufeffDate;Payee;Category;Amount;Currency;Paid by;Note;Alex;Bo\r\n") || !strings.Contains(csv.raw, "03.06.2026;Muzeum;Kultura;30,00;PLN;Bo;;15,00;15,00") {
		t.Errorf("csv = %q", csv.raw)
	}
	tl := alex.do("GET", "/groups/timeline", nil)
	expect(t, tl, 200, "")
	if gs := tl.body["groups"].([]any); len(gs) != 1 || gs[0].(map[string]any)["source"] != "shared" || num(gs[0].(map[string]any)["spentMinor"]) != 9000 {
		t.Errorf("timeline = %s", tl.raw) // the linked personal group shows once, as the shared group
	}
	// settle up: Bo pays Alex; leaving then works
	expect(t, bo.do("POST", "/shared-groups/"+gid+"/leave", nil), 409, "conflict")
	expect(t, out.do("POST", "/shared-groups/"+gid+"/settlements", map[string]any{"from": b, "to": a, "amountMinor": 1500, "settledOn": "2026-06-10"}), 404, "not_found")
	st := bo.do("POST", "/shared-groups/"+gid+"/settlements", map[string]any{"from": b, "to": a, "amountMinor": 1500, "settledOn": "2026-06-10"})
	expect(t, st, 201, "")
	d = bo.do("GET", "/shared-groups/"+gid, nil)
	if balanceOf(d, a) != 0 || balanceOf(d, b) != 0 || len(d.body["transfers"].([]any)) != 0 {
		t.Errorf("after settling: %s", d.raw)
	}
	if n := alex.do("GET", "/notifications", nil); !strings.Contains(n.raw, `"shared.joined"`) || !strings.Contains(n.raw, `"shared.settled"`) || !strings.Contains(n.raw, `"shared.expense_added"`) {
		t.Errorf("alex's notifications = %s", n.raw)
	}
	// the data export carries the group as the user sees it
	z := alex.do("GET", "/me/export", nil)
	expect(t, z, 200, "")
	zr, err := zip.NewReader(bytes.NewReader([]byte(z.raw)), int64(len(z.raw)))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range zr.File {
		if f.Name == "shared-groups.json" {
			rc, _ := f.Open()
			data, _ := io.ReadAll(rc)
			_ = rc.Close()
			found = strings.Contains(string(data), "Muzeum") && !strings.Contains(string(data), "@")
		}
	}
	if !found {
		t.Error("shared-groups.json missing from the export")
	}
	acts := actions(t, bo)
	for _, want := range []string{"shared.joined", "shared.settled"} {
		if !strings.Contains(strings.Join(acts, ","), want) {
			t.Errorf("bo's activity %v lacks %s", acts, want)
		}
	}
	expect(t, bo.do("POST", "/shared-groups/"+gid+"/leave", nil), 204, "")
	expect(t, bo.do("GET", "/shared-groups/"+gid, nil), 404, "not_found")
	// once someone joined, the owner archives instead of deleting
	expect(t, alex.do("DELETE", "/shared-groups/"+gid, nil), 409, "conflict")
	arch := alex.do("PATCH", "/shared-groups/"+gid, map[string]any{"name": "Kraków", "kind": "trip", "archived": true})
	expect(t, arch, 200, "")
	expect(t, alex.do("POST", "/shared-groups/"+gid+"/expenses", map[string]any{"bookedOn": "2026-06-04", "amountMinor": 100}), 409, "conflict")
}

// TestSharedGroupsLimit: the maxSharedGroups limit counts the groups a user owns.
func TestSharedGroupsLimit(t *testing.T) {
	e := newTestEnv(t, nil, false)
	b := named(t, e, "lim", "Lim")
	if _, err := tu.Require(t, testDB).Owner.Exec(context.Background(), `UPDATE users SET limits = '{"maxSharedGroups":1}' WHERE id = $1`, e.userID(t, b)); err != nil {
		t.Fatal(err)
	}
	expect(t, b.do("POST", "/shared-groups", map[string]any{"name": "One"}), 201, "")
	r := b.do("POST", "/shared-groups", map[string]any{"name": "Two"})
	expect(t, r, 422, "limit_reached")
	if me := b.do("GET", "/me", nil); num(obj(t, me.body, "limits")["maxSharedGroups"]) != 1 {
		t.Errorf("me = %s", me.raw)
	}
}

// TestSharedGroupsOff: demo mode and the instance switch hide every shared-group route.
func TestSharedGroupsOff(t *testing.T) {
	e := newTestEnv(t, func(c *config.Config) { c.DemoMode = true }, false)
	b := named(t, e, "demo", "Demo")
	expect(t, b.do("GET", "/shared-groups", nil), 404, "not_found")
	expect(t, b.do("GET", "/groups/timeline", nil), 404, "not_found")

	e2 := newTestEnv(t, nil, false)
	if _, err := tu.Require(t, testDB).Owner.Exec(context.Background(), `UPDATE instance_settings SET doc = '{"features":{"sharedGroups":false}}'`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = tu.Require(t, testDB).Owner.Exec(context.Background(), `UPDATE instance_settings SET doc = '{}'`)
	})
	e2.clock.Add(time.Minute) // past the settings cache
	c := named(t, e2, "off", "Off")
	expect(t, c.do("GET", "/shared-groups", nil), 404, "not_found")
	expect(t, c.do("POST", "/shared-groups", map[string]any{"name": "x"}), 404, "not_found")
	expect(t, c.do("GET", "/groups/timeline", nil), 404, "not_found")
	expect(t, c.do("GET", "/groups", nil), 200, "")
}

// userID is a signed-in browser's user id.
func (e *authEnv) userID(t *testing.T, b *browser) string {
	t.Helper()
	me := b.do("GET", "/me", nil)
	expect(t, me, 200, "")
	return me.body["id"].(string)
}
