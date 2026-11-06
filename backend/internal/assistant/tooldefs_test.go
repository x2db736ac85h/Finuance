package assistant

import (
	"encoding/json"
	"testing"
)

// TestToolDefsAreJSONSchemas: every tool's parameters are a valid JSON object schema (they are
// written as JSON text, so a slip in the escaping would only show at a model server).
func TestToolDefsAreJSONSchemas(t *testing.T) {
	for _, td := range ToolDefs(ToolsetFull) {
		var schema struct {
			Type       string                     `json:"type"`
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(td.Parameters, &schema); err != nil || schema.Type != "object" {
			t.Errorf("%s: %v (%s)", td.Name, err, td.Parameters)
		}
	}
}
