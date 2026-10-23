package httpapi

import (
	"context"

	"github.com/x2db736ac85h/finuance/backend/internal/assistant"
	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/llm"
)

// AssistantStatus lists the connections and their chat models.
func (a *api) AssistantStatus(ctx context.Context, req gen.AssistantStatusRequestObject) (gen.AssistantStatusResponseObject, error) {
	out := gen.AssistantStatus200JSONResponse{Enabled: a.conns.Enabled(), Connections: []gen.AssistantConnection{}, MaxRowsLimit: assistant.MaxRowsLimit}
	for _, st := range a.conns.Status(ctx, req.Params.Refresh != nil && *req.Params.Refresh) {
		c := gen.AssistantConnection{Name: st.Name, Label: st.Label, Api: gen.AssistantConnectionApi(st.API), Cloud: st.Cloud,
			Reachable: st.Reachable, Models: make([]gen.AssistantModel, 0, len(st.Models)), Error: assistantError(st.Err)}
		if st.DefaultModel != "" {
			c.DefaultModel = &st.DefaultModel
		}
		for _, m := range st.Models {
			c.Models = append(c.Models, gen.AssistantModel{Id: m.ID, Tools: m.Caps.Tools,
				Reasoning: gen.AssistantModelReasoning(m.Caps.Reasoning), ContextTokens: m.Caps.ContextTokens})
		}
		out.Connections = append(out.Connections, c)
	}
	return out, nil
}

func assistantError(e *llm.Error) *gen.AssistantError {
	if e == nil {
		return nil
	}
	return &gen.AssistantError{Kind: gen.AssistantErrorKind(e.Kind), Message: e.Message}
}
