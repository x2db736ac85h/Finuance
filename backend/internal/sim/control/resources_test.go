package control

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/sim/control/gen"
)

// fakePrometheus answers query_range with a matrix per kind of query: two containers by CPU and
// memory (the sidecar's memory larger), one series for the others, and a NaN to leave out.
func fakePrometheus(t *testing.T, queries *[]string, calls *atomic.Int64) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		q := r.URL.Query().Get("query")
		*queries = append(*queries, q)
		if r.URL.Path != "/api/v1/query_range" || r.URL.Query().Get("step") != "15" {
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		series := func(labels string, values ...string) string {
			var pts []string
			for i, v := range values {
				pts = append(pts, fmt.Sprintf(`[%d,%q]`, 1795400000+15*i, v))
			}
			return fmt.Sprintf(`{"metric":%s,"values":[%s]}`, labels, strings.Join(pts, ","))
		}
		var result []string
		switch {
		case strings.Contains(q, "container_memory_working_set_bytes"):
			result = []string{series(`{"container":"api"}`, "100", "120"), series(`{"container":"db"}`, "500", "600")}
		case strings.Contains(q, "container_cpu_usage_seconds_total"):
			result = []string{series(`{"container":"api"}`, "0.1", "NaN", "0.3"), series(`{"container":"db"}`, "0.2", "0.2")}
		default:
			result = []string{series(`{}`, "1", "2")}
		}
		_, _ = fmt.Fprintf(w, `{"status":"success","data":{"resultType":"matrix","result":[%s]}}`, strings.Join(result, ","))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestResources: the live strip's figures come from fixed queries over the lab's namespace and the
// simulator's, containers ordered by memory, a NaN left out, one fetch for every viewer within the
// cache, and no Prometheus is said plainly.
func TestResources(t *testing.T) {
	var queries []string
	var calls atomic.Int64
	prom := fakePrometheus(t, &queries, &calls)
	now := time.Date(2026, 11, 23, 12, 0, 7, 0, time.UTC)
	r, err := newResources(prom.URL, "finuance-lab", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	got := r.get(context.Background())
	if !got.Available || len(got.Containers) != 2 || got.Containers[0].Name != "db" || len(got.Series) != 4 {
		t.Fatalf("figures: %+v", got)
	}
	if api := got.Containers[1]; len(api.Cpu) != 2 || api.Cpu[1].V != 0.3 || api.MemoryBytes[1].V != 120 {
		t.Errorf("the api's figures: %+v", api)
	}
	for _, s := range got.Series {
		if len(s.Points) != 2 {
			t.Errorf("%s: %+v", s.Name, s.Points)
		}
	}
	for _, q := range queries {
		if strings.Contains(q, "container_") && !strings.Contains(q, `namespace=~"finuance-lab|finuance-sim"`) ||
			strings.Contains(q, "http_server_") && !strings.Contains(q, `namespace="finuance-lab"`) {
			t.Errorf("a query outside the lab: %s", q)
		}
	}
	before := calls.Load()
	now = now.Add(4 * time.Second)
	r.get(context.Background())
	if calls.Load() != before {
		t.Error("a second viewer within the cache queried Prometheus again")
	}
	now = now.Add(2 * time.Second)
	r.get(context.Background())
	if calls.Load() == before {
		t.Error("the cache never expired")
	}

	none, err := newResources("", "", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if g := none.get(context.Background()); g.Available || g.Error == nil || g.Containers == nil {
		t.Errorf("without Prometheus: %+v", g)
	}
	down, _ := newResources("http://127.0.0.1:1", "", time.Now)
	if g := down.get(context.Background()); g.Available || g.Error == nil || *g.Error != "the lab's Prometheus does not answer" {
		t.Errorf("Prometheus down: %+v", g)
	}
	for _, c := range []struct{ url, ns string }{{"ftp://x", ""}, {"http://user:pw@x", ""}, {"http://x", `lab"}) or vector(1`}, {"http://x", "Lab"}} {
		if _, err := newResources(c.url, c.ns, time.Now); err == nil {
			t.Errorf("%q, %q accepted", c.url, c.ns)
		}
	}
}

// TestResourcesRoute: the route answers the contract's document behind the control token.
func TestResourcesRoute(t *testing.T) {
	srv, _, _ := newServer(t)
	resp, body := call(t, srv, http.MethodGet, "/api/resources", nil, nil)
	var got gen.Resources
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &got) != nil || got.Available {
		t.Errorf("%d %s", resp.StatusCode, body)
	}
	resp, _ = call(t, srv, http.MethodGet, "/api/resources", nil, func(r *http.Request) { r.Header.Del("Authorization") })
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("without the token: %d", resp.StatusCode)
	}
}
