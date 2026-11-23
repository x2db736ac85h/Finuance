package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/x2db736ac85h/finuance/backend/internal/sim/identity"
)

// TestUsers: the credentials file holds the first N animals with their passwords, readable by this
// user only, and replaces an older file.
func TestUsers(t *testing.T) {
	seed := strings.Repeat("s", identity.MinSeedBytes)
	path := filepath.Join(t.TempDir(), "users.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	var errb bytes.Buffer
	if code := run(context.Background(), []string{"users", "-count=12", "-out=" + path},
		func(k string) string { return map[string]string{"FINUANCE_SIM_SEED": seed}[k] }, io.Discard, &errb); code != 0 {
		t.Fatalf("users: %d %s", code, errb.String())
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the file's mode: %v %v", info.Mode(), err)
	}
	var creds []struct{ Email, Password string }
	b, _ := os.ReadFile(path)
	if err := json.Unmarshal(b, &creds); err != nil || len(creds) != 12 {
		t.Fatalf("%d credentials: %v", len(creds), err)
	}
	want, _ := identity.Password([]byte(seed), "otter-2")
	if creds[10].Email != "otter-2@sim.invalid" || creds[10].Password != want {
		t.Errorf("the eleventh: %+v", creds[10])
	}
}

func TestCommands(t *testing.T) {
	seed := strings.Repeat("s", identity.MinSeedBytes)
	env := func(k string) string {
		if k == "FINUANCE_SIM_SEED" {
			return seed
		}
		return ""
	}
	want, _ := identity.Password([]byte(seed), "fox")
	for _, c := range []struct {
		args     []string
		env      func(string) string
		code     int
		out, err string
	}{
		{[]string{"password", "fox"}, env, 0, "fox@sim.invalid\t" + want + "\n", ""},
		{[]string{"password", "unicorn"}, env, 2, "", "no persona"},
		{[]string{"password", "fox"}, func(string) string { return "short" }, 1, "", "FINUANCE_SIM_SEED"},
		{[]string{"password"}, env, 2, "", "usage"},
		{[]string{"run"}, env, 1, "", "the target must be"},
		{[]string{"run", "-target=https://lab.example", "-tz=Mars/Base"}, env, 2, "", "-tz"},
		{[]string{"run", "-target=https://lab.example", "-ca-file=/nonexistent/ca.pem"}, env, 2, "", "-ca-file"},
		{[]string{"run", "-target=https://lab.example", "-ca-file=main.go"}, env, 2, "", "no PEM certificate"},
		{[]string{"seed"}, env, 1, "", "the target must be"},
		{[]string{"seed", "-target=https://lab.example", "-count=501"}, env, 2, "", "-count must be"},
		{[]string{"seed", "-target=https://lab.example", "-ca-file=main.go"}, env, 2, "", "no PEM certificate"},
		{[]string{"users"}, env, 2, "", "usage"},
		{[]string{"users", "-out=x.json", "-count=0"}, env, 2, "", "between 1 and"},
		{[]string{"version"}, env, 0, "finuance-sim dev\n", ""},
		{[]string{"fly"}, env, 2, "", "unknown command"},
		{nil, env, 2, "", "usage"},
	} {
		var out, errb bytes.Buffer
		code := run(context.Background(), c.args, c.env, &out, &errb)
		if code != c.code || (c.out != "" && out.String() != c.out) || (c.err != "" && !strings.Contains(errb.String(), c.err)) {
			t.Errorf("%v: %d, %q, %q", c.args, code, out.String(), errb.String())
		}
		if strings.Contains(errb.String(), seed) || (len(c.args) > 0 && c.args[0] != "password" && strings.Contains(out.String(), want)) {
			t.Errorf("%v printed the seed or a password", c.args)
		}
	}
}
