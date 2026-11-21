package observability

import (
	"os"
	"regexp"
	"slices"
	"testing"
)

// TestCollectorDropsTheSameAttributes: Alloy deletes the same span attributes as Redact does in the
// process (PLAN 24.6, ADR 0042), so a span from an exporter that bypassed Redact still leaves the
// cluster's collector without them. The list is in deploy/k8s/telemetry/files/traces.alloy.
func TestCollectorDropsTheSameAttributes(t *testing.T) {
	cfg, err := os.ReadFile("../../../deploy/k8s/telemetry/files/traces.alloy")
	if err != nil {
		t.Fatal(err)
	}
	block := regexp.MustCompile(`(?s)otelcol\.processor\.attributes "redact" \{(.*?)\n\toutput \{`).FindSubmatch(cfg)
	if block == nil {
		t.Fatal(`traces.alloy has no otelcol.processor.attributes "redact" block`)
	}
	var inAlloy []string
	for _, m := range regexp.MustCompile(`(?s)action \{\s*key\s*=\s*"([^"]+)"\s*action\s*=\s*"delete"\s*\}`).FindAllSubmatch(block[1], -1) {
		inAlloy = append(inAlloy, string(m[1]))
	}
	var inProcess []string
	for k := range droppedSpanAttributes {
		inProcess = append(inProcess, string(k))
	}
	slices.Sort(inAlloy)
	slices.Sort(inProcess)
	if !slices.Equal(inAlloy, inProcess) {
		t.Errorf("traces.alloy deletes %v,\nRedact drops %v", inAlloy, inProcess)
	}
}
