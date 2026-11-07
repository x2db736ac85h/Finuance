package httpapi

import (
	"context"

	"github.com/x2db736ac85h/finuance/backend/internal/assistant"
	"github.com/x2db736ac85h/finuance/backend/internal/config"
	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/llm"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// Administration → Assistant (ADR 0035): the connections added in the panel and per-model tuning.
// API keys go in and never come out.

func (a *api) connectionStore() (*assistant.ConnectionStore, error) {
	if a.aiAdmin == nil {
		return nil, platform.ErrNotFound
	}
	return a.aiAdmin, nil
}

func connectionDTO(c *assistant.StoredConnection) gen.AdminConnection {
	return gen.AdminConnection{Name: c.Name, Label: c.Label, Api: gen.AdminConnectionApi(c.API), BaseUrl: c.BaseURL, Cloud: c.Cloud,
		DefaultModel: c.DefaultModel, Models: c.Models, ContextTokens: c.ContextTokens, Source: gen.AdminConnectionSource(c.Source),
		HasKey: c.HasKey, KeyUnreadable: c.KeyUnreadable, Version: c.Version, UpdatedAt: c.UpdatedAt}
}

func (a *api) auditAssistant(ctx context.Context, meta map[string]any) {
	a.adminAudit(ctx, ledger.AuditAdminAssistantChanged, nil, meta)
}

// AdminListConnections lists the file's and the panel's connections.
func (a *api) AdminListConnections(ctx context.Context, _ gen.AdminListConnectionsRequestObject) (gen.AdminListConnectionsResponseObject, error) {
	s, err := a.connectionStore()
	if err != nil {
		return nil, err
	}
	list, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make(gen.AdminListConnections200JSONResponse, 0, len(list))
	for i := range list {
		out = append(out, connectionDTO(&list[i]))
	}
	return out, nil
}

// AdminCreateConnection adds a connection.
func (a *api) AdminCreateConnection(ctx context.Context, req gen.AdminCreateConnectionRequestObject) (gen.AdminCreateConnectionResponseObject, error) {
	s, err := a.connectionStore()
	if err != nil {
		return nil, err
	}
	b := req.Body
	c, err := s.Create(ctx, config.ConnectionSpec{Name: b.Name, Label: b.Label, API: string(b.Api), BaseURL: b.BaseUrl, Cloud: b.Cloud,
		DefaultModel: b.DefaultModel, Models: b.Models, ContextTokens: b.ContextTokens, APIKey: deref(b.ApiKey)})
	if err != nil {
		return nil, err
	}
	a.auditAssistant(ctx, map[string]any{"connection": c.Name, "change": "added", "cloud": c.Cloud, "key": c.HasKey})
	return gen.AdminCreateConnection201JSONResponse(connectionDTO(&c)), nil
}

// AdminUpdateConnection changes a connection added in the panel.
func (a *api) AdminUpdateConnection(ctx context.Context, req gen.AdminUpdateConnectionRequestObject) (gen.AdminUpdateConnectionResponseObject, error) {
	s, err := a.connectionStore()
	if err != nil {
		return nil, err
	}
	b := req.Body
	key := assistant.KeyChange{}
	switch b.KeyAction {
	case gen.Remove:
		key.Set = true
	case gen.Replace:
		key.Set, key.Value = true, deref(b.ApiKey)
		if key.Value == "" {
			ve := &platform.ValidationError{}
			ve.Add("apiKey", "is needed to replace the key")
			return nil, ve
		}
	case gen.Keep:
	}
	c, err := s.Update(ctx, req.Name, b.Version, config.ConnectionSpec{Label: b.Label, API: string(b.Api), BaseURL: b.BaseUrl, Cloud: b.Cloud,
		DefaultModel: b.DefaultModel, Models: b.Models, ContextTokens: b.ContextTokens}, key)
	if err != nil {
		return nil, err
	}
	a.auditAssistant(ctx, map[string]any{"connection": c.Name, "change": "changed", "cloud": c.Cloud, "key": string(b.KeyAction)})
	return gen.AdminUpdateConnection200JSONResponse(connectionDTO(&c)), nil
}

// AdminDeleteConnection deletes a connection added in the panel.
func (a *api) AdminDeleteConnection(ctx context.Context, req gen.AdminDeleteConnectionRequestObject) (gen.AdminDeleteConnectionResponseObject, error) {
	s, err := a.connectionStore()
	if err != nil {
		return nil, err
	}
	if err := s.Delete(ctx, req.Name); err != nil {
		return nil, err
	}
	a.auditAssistant(ctx, map[string]any{"connection": req.Name, "change": "deleted"})
	return gen.AdminDeleteConnection204Response{}, nil
}

func tuningDTO(t *assistant.Tuning) gen.ModelTuning {
	out := gen.ModelTuning{Connection: t.Connection, Model: t.Model, ExtraPrompt: t.ExtraPrompt, Temperature: t.Temperature,
		ContextTokens: t.ContextTokens}
	if t.Think != "" {
		v := gen.ModelTuningThink(t.Think)
		out.Think = &v
	}
	if t.Toolset != "" {
		v := gen.ModelTuningToolset(t.Toolset)
		out.Toolset = &v
	}
	if !t.UpdatedAt.IsZero() {
		out.UpdatedAt = &t.UpdatedAt
	}
	return out
}

// AdminListTuning lists every model's tuning.
func (a *api) AdminListTuning(ctx context.Context, _ gen.AdminListTuningRequestObject) (gen.AdminListTuningResponseObject, error) {
	s, err := a.connectionStore()
	if err != nil {
		return nil, err
	}
	list, err := s.Tunings(ctx)
	if err != nil {
		return nil, err
	}
	out := make(gen.AdminListTuning200JSONResponse, 0, len(list))
	for i := range list {
		out = append(out, tuningDTO(&list[i]))
	}
	return out, nil
}

// AdminPutTuning saves (or, setting nothing, removes) a model's tuning.
func (a *api) AdminPutTuning(ctx context.Context, req gen.AdminPutTuningRequestObject) (gen.AdminPutTuningResponseObject, error) {
	s, err := a.connectionStore()
	if err != nil {
		return nil, err
	}
	b := req.Body
	t := assistant.Tuning{Connection: b.Connection, Model: b.Model, Temperature: b.Temperature, ContextTokens: b.ContextTokens, ExtraPrompt: b.ExtraPrompt}
	if b.Think != nil {
		t.Think = llm.Thinking(*b.Think)
	}
	if b.Toolset != nil {
		t.Toolset = string(*b.Toolset)
	}
	out, err := s.SetTuning(ctx, t)
	if err != nil {
		return nil, err
	}
	a.auditAssistant(ctx, map[string]any{"connection": t.Connection, "model": t.Model, "change": "tuning"})
	return gen.AdminPutTuning200JSONResponse(tuningDTO(&out)), nil
}
