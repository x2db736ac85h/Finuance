// Package control serves the traffic simulator's control API, its tuning page and its metrics on
// one port (PLAN 25.5, 25.6; api/sim.openapi.yaml). It is not published through an ingress; a
// port-forward reaches it, and so can any page in the same browser (THREAT-MODEL D10), so every
// API route needs the control token, the client header and, when sent, a matching Origin.
package control

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/getkin/kin-openapi/openapi3filter"
	nethttpmiddleware "github.com/oapi-codegen/nethttp-middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/x2db736ac85h/finuance/backend/internal/sim"
	"github.com/x2db736ac85h/finuance/backend/internal/sim/control/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/sim/statements"
)

//go:embed ui
var ui embed.FS

// MinTokenBytes is the shortest control token accepted.
const MinTokenBytes = 32

// csp is the tuning page's Content-Security-Policy: the main app's (deploy/docker/nginx.conf)
// without its report endpoint, which only the main API has.
const csp = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data: blob:; connect-src 'self'; " +
	"object-src 'none'; worker-src 'self'; frame-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'; " +
	"require-trusted-types-for 'script'"

// Options configure the server.
type Options struct {
	Engine  *sim.Engine
	Token   string // FINUANCE_SIM_CONTROL_TOKEN
	Version string
	Grafana string // Grafana's address for the feed's links ("" for none)
	// Prometheus is the lab's Prometheus, for the page's live strip ("" for none); Namespace is
	// the lab's namespace there (finuance on Compose).
	Prometheus string
	Namespace  string
	Registry   *prometheus.Registry
	Log        *slog.Logger
}

// Handler returns the server's handler: /api/ (the control API), /metrics, /healthz and the page.
func Handler(o Options) (http.Handler, error) {
	if len(o.Token) < MinTokenBytes {
		return nil, fmt.Errorf("the control token must be at least %d bytes", MinTokenBytes)
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	spec, err := gen.GetSpec()
	if err != nil {
		return nil, err
	}
	validator := nethttpmiddleware.OapiRequestValidatorWithOptions(spec, &nethttpmiddleware.Options{
		SilenceServersWarning: true,
		Options:               openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc},
		ErrorHandlerWithOpts: func(_ context.Context, err error, w http.ResponseWriter, _ *http.Request, opts nethttpmiddleware.ErrorHandlerOpts) {
			status := opts.StatusCode
			if status == 0 || status == http.StatusBadRequest {
				status = http.StatusUnprocessableEntity
			}
			problem(w, status, "invalid_request", "The request does not fit the control API.", err.Error())
		},
	})
	res, err := newResources(o.Prometheus, o.Namespace, time.Now)
	if err != nil {
		return nil, err
	}
	s := &server{o: o, res: res}
	strict := gen.NewStrictHandlerWithOptions(s, nil, gen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			problem(w, http.StatusUnprocessableEntity, "invalid_request", "The request does not fit the control API.", err.Error())
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			problem(w, http.StatusInternalServerError, "internal", "The simulator could not answer.", err.Error())
		},
	})
	mux := http.NewServeMux()
	api := gen.HandlerWithOptions(strict, gen.StdHTTPServerOptions{BaseURL: "/api", BaseRouter: http.NewServeMux(),
		ErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			problem(w, http.StatusUnprocessableEntity, "invalid_request", "The request does not fit the control API.", err.Error())
		}})
	mux.Handle("/api/", guard(o.Token, validator(api)))
	reg := o.Registry
	if reg == nil {
		reg = prometheus.NewRegistry()
	}
	mux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok\n")) })
	page, err := fs.Sub(ui, "ui")
	if err != nil {
		return nil, err
	}
	files := http.FileServerFS(page)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			problem(w, http.StatusMethodNotAllowed, "method_not_allowed", "The page is read with GET.", "")
			return
		}
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		files.ServeHTTP(w, r)
	})
	return accessLog(o.Log, mux), nil
}

// guard lets a request through with the control token, the client header and, when the browser
// sends one, an Origin that is the simulator's own address.
func guard(token string, next http.Handler) http.Handler {
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Finuance-Client") != "web" {
			problem(w, http.StatusForbidden, "forbidden", "X-Finuance-Client: web is required.", "")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host && origin != "https://"+r.Host {
			problem(w, http.StatusForbidden, "forbidden", "The request came from another site.", "")
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			problem(w, http.StatusUnauthorized, "unauthorized", "The control token is missing or wrong.", "")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func problem(w http.ResponseWriter, status int, code, title, detail string) {
	p := gen.Problem{Status: status, Code: code, Title: title}
	if detail != "" {
		p.Detail = &detail
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(p)
}

// accessLog writes one line per request (never a header: the token stays out of the logs).
func accessLog(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		level := slog.LevelInfo
		if r.URL.Path == "/healthz" || r.URL.Path == "/metrics" {
			level = slog.LevelDebug
		}
		log.LogAttrs(r.Context(), level, "request", slog.String("method", r.Method), slog.String("path", r.URL.Path),
			slog.Int("status", rec.status), slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach the writer (the event stream flushes).
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

type server struct {
	o   Options
	mu  sync.Mutex // makes "compare the generation, then store" one step
	res *resources
}

var _ gen.StrictServerInterface = (*server)(nil)

func (s *server) GetState(context.Context, gen.GetStateRequestObject) (gen.GetStateResponseObject, error) {
	e := s.o.Engine
	cfg, generation := e.Config()
	personas, totals := e.State()
	out := gen.GetState200JSONResponse{Target: e.Target(), Version: s.o.Version, Paused: cfg.Paused, Generation: generation,
		Mode: gen.StateMode(cfg.Mode), Rate: e.Rate(), Totals: counts(totals), Personas: make([]gen.PersonaState, 0, len(personas))}
	if s.o.Grafana != "" {
		out.Grafana = &s.o.Grafana
	}
	if ends := e.LoadEndsAt(); !ends.IsZero() {
		out.LoadEndsAt = &ends
	}
	rebuilt, pending := e.Rebuilds()
	out.Rebuild = gen.RebuildStatus{Generation: rebuilt, Pending: pending}
	out.HistoryEstimate = statements.Estimate(cfg.History)
	for i := range personas {
		p := &personas[i]
		ps := gen.PersonaState{Slug: p.Persona.Slug, Name: p.Persona.Name, Animal: p.Persona.Animal, Says: p.Character.Says,
			On: p.On, SignedIn: p.SignedIn, Missing: p.Missing, Counts: counts(p.Counts)}
		if p.History != "" {
			ps.History = &p.History
		}
		if p.Action != "" {
			ps.Action = &p.Action
		}
		if p.Last != nil {
			ev := event(*p.Last)
			ps.Last = &ev
		}
		out.Personas = append(out.Personas, ps)
	}
	return out, nil
}

func (s *server) GetCatalogue(context.Context, gen.GetCatalogueRequestObject) (gen.GetCatalogueResponseObject, error) {
	out := gen.GetCatalogue200JSONResponse{Areas: sim.Areas, Ceilings: gen.Ceilings{Personas: sim.MaxPersonas, Intensity: sim.MaxIntensity,
		ClockRate: sim.MaxClockRate, AssistantPerDay: sim.MaxAssistantPerDay, ActionsPerMinute: int(time.Minute / sim.MinGap),
		LoadPersonas: sim.MaxLoadPersonas, Rate: sim.MaxRate, RampSeconds: sim.MaxRampSeconds, LoadMinutes: sim.MaxLoadMinutes,
		LoadActionsPerSecond: int(time.Second / sim.MinLoadGap),
		History: gen.History{Density: statements.MaxDensity, Spread: statements.MaxSpread, Variety: statements.MaxVariety,
			Years: statements.MaxYears}}}
	costs := map[sim.Cost]gen.CatalogueActionCost{sim.Light: gen.Light, sim.Heavy: gen.Heavy, sim.Model: gen.Model}
	for _, a := range s.o.Engine.Actions() {
		out.Actions = append(out.Actions, gen.CatalogueAction{Name: a.Name, Area: a.Area, Cost: costs[a.Cost], Mistake: a.Mistake})
	}
	for _, name := range animals() {
		c := sim.Characters[name]
		out.Characters = append(out.Characters, gen.Character{Animal: c.Animal, Says: c.Says, Weights: c.Weights})
	}
	for name := range sim.Presets {
		out.Presets = append(out.Presets, name)
	}
	slices.Sort(out.Presets)
	return out, nil
}

func (s *server) GetConfig(context.Context, gen.GetConfigRequestObject) (gen.GetConfigResponseObject, error) {
	cfg, generation := s.o.Engine.Config()
	return gen.GetConfig200JSONResponse(doc(cfg, generation)), nil
}

func (s *server) PutConfig(_ context.Context, req gen.PutConfigRequestObject) (gen.PutConfigResponseObject, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, generation := s.o.Engine.Config()
	if req.Body.Generation != generation {
		return gen.PutConfig409JSONResponse(doc(cur, generation)), nil
	}
	cfg, g := s.o.Engine.SetConfig(fromAPI(req.Body.Config))
	return gen.PutConfig200JSONResponse(doc(cfg, g)), nil
}

func (s *server) ResetConfig(context.Context, gen.ResetConfigRequestObject) (gen.ResetConfigResponseObject, error) {
	cfg, g := s.change(func(sim.Config) sim.Config { return s.o.Engine.Defaults() })
	return gen.ResetConfig200JSONResponse(doc(cfg, g)), nil
}

func (s *server) ApplyPreset(_ context.Context, req gen.ApplyPresetRequestObject) (gen.ApplyPresetResponseObject, error) {
	preset, ok := sim.Presets[string(req.Name)]
	if !ok {
		return gen.ApplyPresetdefaultJSONResponse{StatusCode: http.StatusNotFound, Body: gen.Problem{Status: http.StatusNotFound,
			Code: "not_found", Title: "No such preset."}}, nil
	}
	cfg, g := s.change(preset)
	return gen.ApplyPreset200JSONResponse(doc(cfg, g)), nil
}

// GetResources answers what the lab costs, from its Prometheus (ADR 0044).
func (s *server) GetResources(ctx context.Context, _ gen.GetResourcesRequestObject) (gen.GetResourcesResponseObject, error) {
	return gen.GetResources200JSONResponse(s.res.get(ctx)), nil
}

// RebuildHistory asks every persona that plays to rebuild its history (ADR 0044).
func (s *server) RebuildHistory(context.Context, gen.RebuildHistoryRequestObject) (gen.RebuildHistoryResponseObject, error) {
	s.o.Engine.RequestRebuild()
	generation, pending := s.o.Engine.Rebuilds()
	return gen.RebuildHistory200JSONResponse{Generation: generation, Pending: pending}, nil
}

func (s *server) Pause(context.Context, gen.PauseRequestObject) (gen.PauseResponseObject, error) {
	cfg, g := s.change(func(c sim.Config) sim.Config { c.Paused = true; return c })
	return gen.Pause200JSONResponse(doc(cfg, g)), nil
}

func (s *server) Resume(context.Context, gen.ResumeRequestObject) (gen.ResumeResponseObject, error) {
	cfg, g := s.change(func(c sim.Config) sim.Config { c.Paused = false; return c })
	return gen.Resume200JSONResponse(doc(cfg, g)), nil
}

func (s *server) change(f func(sim.Config) sim.Config) (sim.Config, int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, _ := s.o.Engine.Config()
	return s.o.Engine.SetConfig(f(cur))
}

func (s *server) ActNow(ctx context.Context, req gen.ActNowRequestObject) (gen.ActNowResponseObject, error) {
	ev, err := s.o.Engine.ActNow(ctx, req.Body.Persona, req.Body.Action)
	switch {
	case errors.Is(err, sim.ErrUnknownPersona), errors.Is(err, sim.ErrUnknownAction):
		return gen.ActNowdefaultJSONResponse{StatusCode: http.StatusNotFound, Body: gen.Problem{Status: http.StatusNotFound,
			Code: "not_found", Title: err.Error() + "."}}, nil
	case err != nil:
		detail := err.Error()
		return gen.ActNowdefaultJSONResponse{StatusCode: http.StatusBadGateway, Body: gen.Problem{Status: http.StatusBadGateway,
			Code: "sign_in_failed", Title: "The persona could not sign in to the lab.", Detail: &detail}}, nil
	}
	return gen.ActNow200JSONResponse(event(ev)), nil
}

func (s *server) StreamEvents(ctx context.Context, _ gen.StreamEventsRequestObject) (gen.StreamEventsResponseObject, error) {
	return stream{ctx: ctx, e: s.o.Engine}, nil
}

// heartbeat keeps an idle stream open through proxies.
const heartbeat = 15 * time.Second

// stream writes the recent actions, then every new one, until the client goes away.
type stream struct {
	ctx context.Context
	e   *sim.Engine
}

func (st stream) VisitStreamEventsResponse(w http.ResponseWriter) error {
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	events, stop := st.e.Subscribe()
	defer stop()
	send := func(ev sim.Event) error {
		data, err := json.Marshal(event(ev))
		if err != nil {
			return err
		}
		if _, err := w.Write([]byte("event: action\ndata: " + string(data) + "\n\n")); err != nil {
			return err
		}
		return rc.Flush()
	}
	recent := st.e.Recent()
	for i := range recent {
		if err := send(recent[i]); err != nil {
			return nil //nolint:nilerr // the client went away
		}
	}
	_ = rc.Flush()
	tick := time.NewTicker(heartbeat)
	defer tick.Stop()
	for {
		select {
		case <-st.ctx.Done():
			return nil
		case ev := <-events:
			if send(ev) != nil {
				return nil
			}
		case <-tick.C:
			if _, err := w.Write([]byte(": keep-alive\n\n")); err != nil || rc.Flush() != nil {
				return nil //nolint:nilerr // the client went away
			}
		}
	}
}

func counts(c sim.Counts) gen.Counts { return gen.Counts{Ok: c.OK, Error: c.Error, Skipped: c.Skipped} }

func event(ev sim.Event) gen.Event {
	out := gen.Event{Time: ev.Time, Persona: ev.Persona, Action: ev.Action, Area: ev.Area, Result: gen.EventResult(ev.Result),
		DurationMs: ev.Duration.Milliseconds()}
	if ev.Status != 0 {
		out.Status = &ev.Status
	}
	if ev.RequestID != "" {
		out.RequestId = &ev.RequestID
	}
	if ev.Error != "" {
		out.Error = &ev.Error
	}
	return out
}

func doc(c sim.Config, generation int64) gen.ConfigDoc {
	out := gen.ConfigDoc{Generation: generation, Config: gen.Config{Mode: gen.ConfigMode(c.Mode), Personas: c.Personas,
		Paused: c.Paused, Intensity: c.Intensity, ActiveHours: c.ActiveHours, ClockRate: c.ClockRate, Mistakes: c.Mistakes,
		AssistantPerDay: c.AssistantPerDay, Rate: c.Rate, RampSeconds: c.RampSeconds, LoadMinutes: c.LoadMinutes,
		PushThrough: c.PushThrough, RunSeed: c.RunSeed, Jitter: c.Jitter, Bursts: c.Bursts,
		History: gen.History{Density: c.History.Density, Spread: c.History.Spread, Variety: c.History.Variety, Years: c.History.Years}}}
	if len(c.Off) > 0 {
		out.Config.Off = &c.Off
	}
	if len(c.AreasOff) > 0 {
		out.Config.AreasOff = &c.AreasOff
	}
	if len(c.Weights) > 0 {
		out.Config.Weights = &c.Weights
	}
	if len(c.PersonaWeights) > 0 {
		out.Config.PersonaWeights = &c.PersonaWeights
	}
	return out
}

func fromAPI(c gen.Config) sim.Config {
	out := sim.Config{Mode: string(c.Mode), Personas: c.Personas, Paused: c.Paused, Intensity: c.Intensity,
		ActiveHours: c.ActiveHours, ClockRate: c.ClockRate, Mistakes: c.Mistakes, AssistantPerDay: c.AssistantPerDay,
		Rate: c.Rate, RampSeconds: c.RampSeconds, LoadMinutes: c.LoadMinutes, PushThrough: c.PushThrough, RunSeed: c.RunSeed,
		Jitter: c.Jitter, Bursts: c.Bursts, History: statements.History{Density: c.History.Density, Spread: c.History.Spread,
			Variety: c.History.Variety, Years: c.History.Years}}
	if c.Off != nil {
		out.Off = *c.Off
	}
	if c.AreasOff != nil {
		out.AreasOff = *c.AreasOff
	}
	if c.Weights != nil {
		out.Weights = *c.Weights
	}
	if c.PersonaWeights != nil {
		out.PersonaWeights = *c.PersonaWeights
	}
	return out
}

func animals() []string { return sim.AnimalOrder() }
