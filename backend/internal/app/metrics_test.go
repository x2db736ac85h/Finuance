package app

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/x2db736ac85h/finuance/backend/internal/assistant"
	"github.com/x2db736ac85h/finuance/backend/internal/jobs"
	"github.com/x2db736ac85h/finuance/backend/internal/observability"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
)

var update = flag.Bool("update", false, "rewrite deploy/k8s/observability/metrics.txt")

// nameRecorder is a Registerer that keeps the names its collectors describe.
type nameRecorder struct{ names map[string]bool }

var fqName = regexp.MustCompile(`fqName: "([^"]+)"`)

func (r *nameRecorder) Register(c prometheus.Collector) error {
	ch := make(chan *prometheus.Desc, 256)
	go func() { c.Describe(ch); close(ch) }()
	for d := range ch {
		if m := fqName.FindStringSubmatch(d.String()); m != nil {
			r.names[m[1]] = true
		}
	}
	return nil
}

func (r *nameRecorder) MustRegister(cs ...prometheus.Collector) {
	for _, c := range cs {
		_ = r.Register(c)
	}
}

func (r *nameRecorder) Unregister(prometheus.Collector) bool { return false }

// TestMetricNames keeps deploy/k8s/observability/metrics.txt equal to the metrics the api and the
// worker register (the collectors Serve and Worker wire up). tools/check-observability.sh checks
// that every metric a dashboard or alert rule uses is in it, so a rename can't silently empty a
// panel or mute an alert. Run with -update after adding or renaming a metric.
func TestMetricNames(t *testing.T) {
	r := &nameRecorder{names: map[string]bool{}}
	r.MustRegister(observability.NewMetrics().Collectors()...)
	r.MustRegister(store.NewPoolCollector(nil)) // Describe doesn't touch the pool
	assistant.NewMetrics(r)                     // api
	jobs.NewImportMetrics(r)                    // worker
	jobs.NewJobMetrics(r)                       // worker
	var names []string
	for n := range r.names {
		if !strings.HasPrefix(n, "go_") && !strings.HasPrefix(n, "process_") { // standard collectors
			names = append(names, n)
		}
	}
	slices.Sort(names)
	got := "# Metrics the api and worker register (TestMetricNames; go_* and process_* are standard).\n" + strings.Join(names, "\n") + "\n"
	path := filepath.Join("..", "..", "..", "deploy", "k8s", "observability", "metrics.txt")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil { //nolint:gosec // a checked-in text file
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Errorf("%s is out of date; run go test ./internal/app -run TestMetricNames -update\n got:\n%s", path, got)
	}
}
