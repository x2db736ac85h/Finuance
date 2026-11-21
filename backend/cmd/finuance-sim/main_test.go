package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/x2db736ac85h/finuance/backend/internal/sim/identity"
)

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
