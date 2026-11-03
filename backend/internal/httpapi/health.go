package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// Check is one readiness probe (database ping, migration version, River client…).
type Check struct {
	Name  string
	Probe func(context.Context) error
}

// HealthzHandler reports that the process is alive.
func HealthzHandler() http.Handler { return http.HandlerFunc(healthz) }

// ReadyzHandler runs every readiness check (used by the worker's admin server too).
func ReadyzHandler(checks []Check) http.Handler { return readyz(checks, platform.SystemClock{}) }

// busyGrace is how long a check that only timed out still counts as passing after its last success.
// Under load every replica waits for the same busy database: failing them all at once would take
// every pod out of the Service together and turn slow answers into refused ones (Phase 18 load test).
const busyGrace = 30 * time.Second

func healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// readyz runs every check with a short timeout. A failure → 503, so a deploy whose migrations are
// behind never receives traffic. A check that only ran out of time (the database is busy, the pool
// full) still passes for busyGrace after its last success and is reported as "busy"; a real error,
// or a timeout beyond the grace, fails at once. Failure messages are generic: no DSNs or SQL leak here.
func readyz(checks []Check, clock platform.Clock) http.Handler {
	var mu sync.Mutex
	lastOK := make(map[string]time.Time, len(checks))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		status, results := http.StatusOK, map[string]string{}
		for _, c := range checks {
			err := c.Probe(ctx)
			now := clock.Now()
			mu.Lock()
			switch {
			case err == nil:
				lastOK[c.Name], results[c.Name] = now, "ok"
			case errors.Is(err, context.DeadlineExceeded) && !lastOK[c.Name].IsZero() && now.Sub(lastOK[c.Name]) <= busyGrace:
				results[c.Name] = "busy"
			default:
				status, results[c.Name] = http.StatusServiceUnavailable, "fail"
			}
			mu.Unlock()
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
