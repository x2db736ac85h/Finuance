package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func runArgs(env map[string]string, args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := run(context.Background(), args, &out, &errb, func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	return code, out.String(), errb.String()
}

func TestDispatch(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		code  int
		inOut string
		inErr string
	}{
		{nil, 2, "", "usage: finuance"},
		{[]string{"version"}, 0, "finuance dev (commit unknown", ""},
		{[]string{"help"}, 0, "migrate up|down|status|version", ""},
		{[]string{"bogus"}, 2, "", `unknown command "bogus"`},
		{[]string{"migrate"}, 2, "", "usage: finuance migrate"},
		{[]string{"seed-demo", "-email=a@b.c"}, 1, "", "not yet implemented (Phase 9)"},
		{[]string{"admin"}, 1, "", "not yet implemented (Phase 5)"},
		{[]string{"serve", "-nope"}, 2, "", "flag provided but not defined"},
	} {
		code, out, errOut := runArgs(nil, tc.args...)
		if code != tc.code || !strings.Contains(out, tc.inOut) || !strings.Contains(errOut, tc.inErr) {
			t.Errorf("%v → %d\nstdout: %s\nstderr: %s", tc.args, code, out, errOut)
		}
	}
}

// Acceptance (Phase 2): an invalid env var stops startup with a clear message naming the variable.
func TestInvalidConfigStopsStartup(t *testing.T) {
	code, _, errOut := runArgs(map[string]string{"FINUANCE_DATABASE_URL": "postgres://u:p@db/x", "FINUANCE_PUBLIC_ORIGIN": "http://localhost:8000",
		"FINUANCE_SESSION_TTL": "forever"}, "serve")
	if code != 1 || !strings.Contains(errOut, "FINUANCE_SESSION_TTL: must be a positive duration") {
		t.Errorf("code %d, stderr %q", code, errOut)
	}
	code, _, errOut = runArgs(nil, "serve")
	if code != 1 || !strings.Contains(errOut, "FINUANCE_PUBLIC_ORIGIN: is required") || !strings.Contains(errOut, "FINUANCE_DATABASE_URL: is required") {
		t.Errorf("missing required: code %d, stderr %q", code, errOut)
	}
	code, _, errOut = runArgs(nil, "migrate", "up")
	if code != 1 || !strings.Contains(errOut, "FINUANCE_MIGRATE_DATABASE_URL: is required") || strings.Contains(errOut, "PUBLIC_ORIGIN") {
		t.Errorf("migrate requirements: code %d, stderr %q", code, errOut)
	}
}
