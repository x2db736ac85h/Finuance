package sim

import (
	"flag"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

var update = flag.Bool("update", false, "rewrite deploy/k8s/observability/sim-metrics.txt")

const simMetricsFile = "../../../deploy/k8s/observability/sim-metrics.txt"

// TestSimMetricNames keeps deploy/k8s/observability/sim-metrics.txt equal to the metrics the
// simulator registers, so tools/check-observability.sh knows them (go test -update rewrites it).
func TestSimMetricNames(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	m.observe(Event{Persona: "otter", Action: "table.window", Result: ResultOK})
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, mf := range mfs {
		names = append(names, mf.GetName())
	}
	slices.Sort(names)
	want := "# Metrics the traffic simulator registers (TestSimMetricNames; go_* and process_* are standard).\n" +
		strings.Join(names, "\n") + "\n"
	if *update {
		if err := os.WriteFile(simMetricsFile, []byte(want), 0o644); err != nil { //nolint:gosec // a checked-in text file
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(simMetricsFile)
	if err != nil || string(got) != want {
		t.Errorf("%s is out of date (go test ./internal/sim/ -run TestSimMetricNames -update):\n%s", simMetricsFile, want)
	}
}
