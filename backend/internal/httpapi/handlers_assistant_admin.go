package httpapi

import (
	"context"
	"errors"
	"time"

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
		HasKey: c.HasKey, KeyUnreadable: c.KeyUnreadable, Version: c.Version, UpdatedAt: c.UpdatedAt, Controls: controlsDTO(c.Controls)}
}

func controlsDTO(c assistant.Controls) gen.AdminConnectionControls {
	out := gen.AdminConnectionControls{Enabled: c.Enabled, DefaultModel: c.DefaultModel, PhotoModel: c.PhotoModel, Version: c.Version,
		UpdatedAt: c.UpdatedAt}
	if c.UnloadAfter > 0 {
		m := int(c.UnloadAfter / time.Minute)
		out.UnloadAfterMinutes = &m
	}
	return out
}

// AdminPutConnectionControls switches a connection on or off and sets its models and idle time.
// Switching it off also unloads what its server holds (Ollama), so the memory is free at once.
func (a *api) AdminPutConnectionControls(ctx context.Context, req gen.AdminPutConnectionControlsRequestObject) (gen.AdminPutConnectionControlsResponseObject, error) {
	s, err := a.connectionStore()
	if err != nil {
		return nil, err
	}
	b := req.Body
	was := a.conns().ControlsOf(req.Name).Enabled
	c, err := s.SetControls(ctx, req.Name, assistant.ControlsInput{Enabled: b.Enabled, UnloadAfterMinutes: b.UnloadAfterMinutes,
		DefaultModel: b.DefaultModel, PhotoModel: b.PhotoModel, Version: b.Version})
	if err != nil {
		return nil, err
	}
	meta := map[string]any{"connection": c.Name, "change": "controls", "enabled": b.Enabled, "unloadAfterMinutes": b.UnloadAfterMinutes,
		"defaultModel": b.DefaultModel, "photoModel": b.PhotoModel}
	if was && !b.Enabled {
		meta["unloaded"] = a.conns().UnloadAll(ctx, c.Name) == nil
	}
	a.auditAssistant(ctx, meta)
	return gen.AdminPutConnectionControls200JSONResponse(connectionDTO(&c)), nil
}

// AdminConnectionModels reports a connection's server: its kind, its models and which are loaded.
func (a *api) AdminConnectionModels(ctx context.Context, req gen.AdminConnectionModelsRequestObject) (gen.AdminConnectionModelsResponseObject, error) {
	if _, err := a.connectionStore(); err != nil {
		return nil, err
	}
	out, err := a.modelServer(ctx, req.Name, req.Params.Refresh != nil && *req.Params.Refresh)
	if err != nil {
		return nil, err
	}
	return gen.AdminConnectionModels200JSONResponse(out), nil
}

// AdminLoadModel loads a model now (Ollama).
func (a *api) AdminLoadModel(ctx context.Context, req gen.AdminLoadModelRequestObject) (gen.AdminLoadModelResponseObject, error) {
	out, err := a.modelAction(ctx, req.Name, req.Body.Model, "load", a.conns().LoadModel)
	if err != nil {
		return nil, err
	}
	return gen.AdminLoadModel200JSONResponse(out), nil
}

// AdminUnloadModel frees a model now (Ollama).
func (a *api) AdminUnloadModel(ctx context.Context, req gen.AdminUnloadModelRequestObject) (gen.AdminUnloadModelResponseObject, error) {
	out, err := a.modelAction(ctx, req.Name, req.Body.Model, "unload", a.conns().UnloadModel)
	if err != nil {
		return nil, err
	}
	return gen.AdminUnloadModel200JSONResponse(out), nil
}

func (a *api) modelAction(ctx context.Context, name, model, action string, do func(context.Context, string, string) error) (gen.AdminModelServer, error) {
	if _, err := a.connectionStore(); err != nil {
		return gen.AdminModelServer{}, err
	}
	if _, err := a.conns().GetAny(name); err != nil {
		return gen.AdminModelServer{}, platform.ErrNotFound
	}
	err := do(ctx, name, model)
	switch {
	case errors.Is(err, llm.ErrCannotManage):
		return gen.AdminModelServer{}, modelProblem("this server doesn't load or unload models from here (Ollama does)")
	case errors.Is(err, assistant.ErrUnknownModel):
		return gen.AdminModelServer{}, modelProblem("isn't a model this connection offers")
	case err != nil:
		var le *llm.Error
		if errors.As(err, &le) {
			return gen.AdminModelServer{}, modelProblem("the server answered: " + le.Message)
		}
		return gen.AdminModelServer{}, err
	}
	a.auditAssistant(ctx, map[string]any{"connection": name, "change": action, "model": model})
	return a.modelServer(ctx, name, true)
}

func modelProblem(msg string) error {
	ve := &platform.ValidationError{}
	ve.Add("model", msg)
	return ve
}

// modelServer is a connection's state as Administration shows it: every offered model and any
// other model the server has loaded, with sizes and unload times where the server says.
func (a *api) modelServer(ctx context.Context, name string, refresh bool) (gen.AdminModelServer, error) {
	st, err := a.conns().StatusOf(ctx, name, refresh)
	if err != nil {
		return gen.AdminModelServer{}, platform.ErrNotFound
	}
	_, m, err := a.conns().Manager(ctx, name)
	if err != nil {
		return gen.AdminModelServer{}, err
	}
	out := gen.AdminModelServer{Server: gen.AdminModelServerServer(st.Server), Reachable: st.Reachable, CanManage: m != nil,
		Models: []gen.AdminServerModel{}, Error: assistantError(st.Err)}
	if st.Reachable {
		ms := int(st.Latency / time.Millisecond)
		out.LatencyMs = &ms
	}
	loaded := map[string]int{}
	for i, l := range st.Loaded {
		loaded[l.ID] = i
	}
	seen := map[string]bool{}
	for _, mi := range st.Models {
		sm := gen.AdminServerModel{Id: mi.ID, Offered: true, Tools: mi.Caps.Tools, Vision: mi.Caps.Vision}
		if st.LoadedKnown {
			i, ok := loaded[mi.ID]
			sm.Loaded = &ok
			if ok {
				withLoaded(&sm, st.Loaded[i])
			}
		}
		seen[mi.ID] = true
		out.Models = append(out.Models, sm)
	}
	for _, l := range st.Loaded {
		if !seen[l.ID] {
			yes := true
			sm := gen.AdminServerModel{Id: l.ID, Loaded: &yes}
			withLoaded(&sm, l)
			out.Models = append(out.Models, sm)
		}
	}
	return out, nil
}

func withLoaded(sm *gen.AdminServerModel, l llm.LoadedModel) {
	if l.SizeBytes > 0 {
		sm.SizeBytes = &l.SizeBytes
	}
	if l.VRAMBytes > 0 {
		sm.VramBytes = &l.VRAMBytes
	}
	if !l.Until.IsZero() {
		sm.UnloadsAt = &l.Until
	}
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
