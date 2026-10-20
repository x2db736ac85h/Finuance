package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// Check is one readiness probe (database ping, migration version, River client…).
type Check struct {
	Name  string
	Probe func(context.Context) error
}

// HealthzHandler reports that the process is alive.
func HealthzHandler() http.Handler { return http.HandlerFunc(healthz) }

// ReadyzHandler runs every readiness check (used by the worker's admin server too).
func ReadyzHandler(checks []Check) http.Handler { return readyz(checks) }

func healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// readyz runs every check with a short timeout. Any failure → 503, so a deploy whose migrations are
// behind never receives traffic. Failure messages are generic: no DSNs or SQL leak here.
func readyz(checks []Check) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		status, results := http.StatusOK, map[string]string{}
		for _, c := range checks {
			if err := c.Probe(ctx); err != nil {
				status, results[c.Name] = http.StatusServiceUnavailable, "fail"
				continue
			}
			results[c.Name] = "ok"
		}
		body := map[string]any{"status": "ok", "checks": results}
		if status != http.StatusOK {
			body["status"] = "unavailable"
		}
		writeJSON(w, status, body)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
