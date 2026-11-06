package assistant

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/x2db736ac85h/finuance/backend/internal/config"
	"github.com/x2db736ac85h/finuance/backend/internal/llm"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/internal/secret"
	"github.com/x2db736ac85h/finuance/backend/internal/store"
	"github.com/x2db736ac85h/finuance/backend/internal/store/db"
)

// Administration → Assistant (ADR 0035): connections an administrator adds in the panel, next to
// the read-only ones of the connections file, and per-model tuning. API keys are sealed under the
// instance key and never leave the server. Every api and worker process reloads them every minute
// (ReloadEvery) and right after a change it made itself.

// ReloadEvery is how often a process reads the stored connections again.
const ReloadEvery = time.Minute

// Connection sources.
const (
	SourceFile  = "file"
	SourcePanel = "panel"
)

// StoredConnection is a connection as the panel shows it: never its key.
type StoredConnection struct {
	Name, Label, API, BaseURL, DefaultModel string
	Cloud                                   bool
	Models                                  []string
	ContextTokens                           int
	Source                                  string // file or panel
	HasKey                                  bool
	KeyUnreadable                           bool  // sealed under another instance key
	Version                                 int32 // 0 for the file's
	UpdatedAt                               *time.Time
}

// KeyChange is what an edit does to the API key: keep it, remove it or set a new one.
type KeyChange struct {
	Set   bool
	Value string // "" with Set removes it
}

// ConnectionStore reads and writes the stored connections and tuning.
type ConnectionStore struct {
	st      *store.Store
	box     *secret.Box // nil without FINUANCE_INSTANCE_KEY
	clock   platform.Clock
	conns   *Connections
	context int // the default context size (FINUANCE_AI_CONTEXT_TOKENS)
	file    []config.AIConnection
	logger  *slog.Logger
	resolve func(ctx context.Context, host string) (bool, error) // llm.CheckHost; tests replace it
}

// NewConnectionStore returns the store for conns. key is FINUANCE_INSTANCE_KEY (nil: keys can't be
// stored); file are the connections file's connections.
func NewConnectionStore(st *store.Store, key []byte, clock platform.Clock, conns *Connections, file []config.AIConnection,
	defaultContext int, logger *slog.Logger) (*ConnectionStore, error) {
	var box *secret.Box
	if key != nil {
		var err error
		if box, err = secret.New(key); err != nil {
			return nil, err
		}
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &ConnectionStore{st: st, box: box, clock: clock, conns: conns, context: defaultContext, file: file, logger: logger,
		resolve: func(ctx context.Context, host string) (bool, error) { return llm.CheckHost(ctx, nil, host) }}, nil
}

// Load reads the stored connections and tuning into the registry. A key that doesn't open (another
// instance key) leaves its connection without a key, so its requests fail with the server's own
// authentication error; the panel says so.
func (s *ConnectionStore) Load(ctx context.Context) error {
	var (
		rows []db.AssistantConnection
		tun  []db.AssistantModelSetting
	)
	if err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) (err error) {
		if rows, err = q.ListStoredConnections(ctx); err != nil {
			return err
		}
		tun, err = q.ListModelSettings(ctx)
		return err
	}); err != nil {
		return err
	}
	var conns []config.AIConnection
	for i := range rows {
		c, err := s.connectionOf(&rows[i])
		if err != nil {
			s.logger.WarnContext(ctx, "assistant connection skipped", "connection", rows[i].Name, "error", err)
			continue
		}
		conns = append(conns, c)
	}
	tuning := make([]Tuning, len(tun))
	for i := range tun {
		tuning[i] = tuningOf(&tun[i])
	}
	s.conns.SetStored(conns, tuning)
	return nil
}

// Run reloads every ReloadEvery until ctx ends (other replicas' changes arrive within a minute).
func (s *ConnectionStore) Run(ctx context.Context) {
	t := time.NewTicker(ReloadEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.Load(ctx); err != nil && ctx.Err() == nil {
				s.logger.WarnContext(ctx, "assistant connections not reloaded", "error", err)
			}
		}
	}
}

func (s *ConnectionStore) connectionOf(r *db.AssistantConnection) (config.AIConnection, error) {
	u, err := url.Parse(r.BaseUrl)
	if err != nil {
		return config.AIConnection{}, err
	}
	c := config.AIConnection{Name: r.Name, Label: r.Label, API: llm.API(r.Api), BaseURL: u, Cloud: r.Cloud, DefaultModel: r.DefaultModel,
		Models: r.Models, ContextTokens: int(r.ContextTokens)}
	if c.ContextTokens == 0 {
		c.ContextTokens = s.context
	}
	if r.ApiKey != nil {
		if key, err := s.box.Open(r.ApiKey, r.Name); err == nil {
			c.APIKey = key
		}
	}
	return c, nil
}

func tuningOf(r *db.AssistantModelSetting) Tuning {
	t := Tuning{Connection: r.Connection, Model: r.Model, ExtraPrompt: r.ExtraPrompt, UpdatedAt: r.UpdatedAt}
	if r.Temperature != nil {
		v := math.Round(float64(*r.Temperature)*100) / 100 // stored as real: 0.7 reads back as 0.699999988
		t.Temperature = &v
	}
	if r.ContextTokens != nil {
		v := int(*r.ContextTokens)
		t.ContextTokens = &v
	}
	if r.Think != nil {
		t.Think = llm.Thinking(*r.Think)
	}
	if r.Toolset != nil {
		t.Toolset = *r.Toolset
	}
	return t
}

// List returns the file's connections (read only) and the stored ones, without keys.
func (s *ConnectionStore) List(ctx context.Context) ([]StoredConnection, error) {
	out := make([]StoredConnection, 0, len(s.file))
	for i := range s.file {
		f := &s.file[i]
		out = append(out, StoredConnection{Name: f.Name, Label: f.Label, API: string(f.API), BaseURL: f.BaseURL.String(), DefaultModel: f.DefaultModel,
			Cloud: f.Cloud, Models: nonNil(f.Models), ContextTokens: f.ContextTokens, Source: SourceFile, HasKey: f.APIKey != ""})
	}
	err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		rows, err := q.ListStoredConnections(ctx)
		for i := range rows {
			out = append(out, s.storedOf(&rows[i]))
		}
		return err
	})
	return out, err
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (s *ConnectionStore) storedOf(r *db.AssistantConnection) StoredConnection {
	sc := StoredConnection{Name: r.Name, Label: r.Label, API: r.Api, BaseURL: r.BaseUrl, DefaultModel: r.DefaultModel, Cloud: r.Cloud,
		Models: nonNil(r.Models), ContextTokens: int(r.ContextTokens), Source: SourcePanel, HasKey: r.ApiKey != nil, Version: r.Version,
		UpdatedAt: &r.UpdatedAt}
	if r.ApiKey != nil {
		_, err := s.box.Open(r.ApiKey, r.Name)
		sc.KeyUnreadable = err != nil
	}
	return sc
}

func problems(m map[string]string) error {
	if len(m) == 0 {
		return nil
	}
	ve := &platform.ValidationError{}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		ve.Add(k, m[k])
	}
	return ve
}

// check validates a connection and, for a local one, that its host doesn't resolve to a public
// address (a host that doesn't resolve yet is allowed; every dial is checked again).
func (s *ConnectionStore) check(ctx context.Context, spec config.ConnectionSpec) (config.AIConnection, error) {
	c, bad := config.NewConnection(spec, s.context)
	if bad == nil {
		bad = map[string]string{}
	}
	if slices.Contains(s.conns.FileNames(), spec.Name) {
		bad["name"] = "is a connection of the configuration file"
	}
	if len(bad) == 0 && !c.Cloud {
		if ok, err := s.resolve(ctx, c.BaseURL.Hostname()); err == nil && !ok {
			bad["baseUrl"] = c.BaseURL.Hostname() + " resolves to a public address (mark the connection as a cloud service if that is intended)"
		}
	}
	return c, problems(bad)
}

func (s *ConnectionStore) seal(name, key string) ([]byte, error) {
	if key == "" {
		return nil, nil
	}
	if s.box == nil {
		return nil, problems(map[string]string{"apiKey": "this instance has no FINUANCE_INSTANCE_KEY to keep API keys with"})
	}
	return s.box.Seal(key, name)
}

func conflict(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return problems(map[string]string{"name": "is taken by another connection"})
	}
	return err
}

// Create stores a new connection and reloads this process's registry.
func (s *ConnectionStore) Create(ctx context.Context, spec config.ConnectionSpec) (StoredConnection, error) {
	c, err := s.check(ctx, spec)
	if err != nil {
		return StoredConnection{}, err
	}
	key, err := s.seal(c.Name, spec.APIKey)
	if err != nil {
		return StoredConnection{}, err
	}
	var out StoredConnection
	err = s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		r, err := q.InsertStoredConnection(ctx, db.InsertStoredConnectionParams{Name: c.Name, Label: c.Label, Api: string(c.API),
			BaseUrl: c.BaseURL.String(), Cloud: c.Cloud, DefaultModel: c.DefaultModel, Models: nonNil(c.Models),
			ContextTokens: int32(spec.ContextTokens), ApiKey: key, Now: s.clock.Now()}) //nolint:gosec // ≤ 10,000,000
		if err == nil {
			out = s.storedOf(&r)
		}
		return err
	})
	if err != nil {
		return StoredConnection{}, conflict(err)
	}
	return out, s.Load(ctx)
}

// Update replaces a stored connection if version is the stored one (platform.ErrStaleVersion
// otherwise); the key is kept, removed or replaced as asked.
func (s *ConnectionStore) Update(ctx context.Context, name string, version int32, spec config.ConnectionSpec, key KeyChange) (StoredConnection, error) {
	spec.Name = name
	if key.Set {
		spec.APIKey = key.Value
	}
	c, err := s.check(ctx, spec)
	if err != nil {
		return StoredConnection{}, err
	}
	var out StoredConnection
	err = s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		cur, err := q.GetStoredConnection(ctx, name)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("connection %q: %w", name, platform.ErrNotFound)
		}
		if err != nil {
			return err
		}
		sealed := cur.ApiKey
		switch {
		case key.Set:
			if sealed, err = s.seal(name, key.Value); err != nil {
				return err
			}
		case c.Cloud != cur.Cloud || c.BaseURL.String() != cur.BaseUrl:
			// a kept key goes only where it went before: a new address needs the key again
			sealed = nil
		}
		r, err := q.UpdateStoredConnection(ctx, db.UpdateStoredConnectionParams{Name: name, Version: version, Label: c.Label, Api: string(c.API),
			BaseUrl: c.BaseURL.String(), Cloud: c.Cloud, DefaultModel: c.DefaultModel, Models: nonNil(c.Models),
			ContextTokens: int32(spec.ContextTokens), ApiKey: sealed, Now: s.clock.Now()}) //nolint:gosec // ≤ 10,000,000
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("connection %q: %w", name, platform.ErrStaleVersion)
		}
		if err == nil {
			out = s.storedOf(&r)
		}
		return err
	})
	if err != nil {
		return StoredConnection{}, err
	}
	return out, s.Load(ctx)
}

// Delete removes a stored connection (its tuning stays, for a connection added again).
func (s *ConnectionStore) Delete(ctx context.Context, name string) error {
	err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		n, err := q.DeleteStoredConnection(ctx, name)
		if err == nil && n == 0 {
			return fmt.Errorf("connection %q: %w", name, platform.ErrNotFound)
		}
		return err
	})
	if err != nil {
		return err
	}
	return s.Load(ctx)
}

// Tunings lists every model's tuning.
func (s *ConnectionStore) Tunings(ctx context.Context) ([]Tuning, error) {
	var out []Tuning
	err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		rows, err := q.ListModelSettings(ctx)
		for i := range rows {
			out = append(out, tuningOf(&rows[i]))
		}
		return err
	})
	return out, err
}

// MaxExtraPrompt is the longest note for one model.
const MaxExtraPrompt = 1000

// SetTuning saves a model's tuning; one that sets nothing is removed.
func (s *ConnectionStore) SetTuning(ctx context.Context, t Tuning) (Tuning, error) {
	t.ExtraPrompt = strings.TrimSpace(t.ExtraPrompt)
	bad := map[string]string{}
	if t.Connection == "" || len(t.Connection) > 40 {
		bad["connection"] = "must name a connection"
	}
	if t.Model == "" || len(t.Model) > 200 {
		bad["model"] = "must name a model"
	}
	if v := t.Temperature; v != nil && (*v < 0 || *v > 2) {
		bad["temperature"] = "must be between 0 and 2"
	}
	if v := t.ContextTokens; v != nil && (*v < 1024 || *v > 10_000_000) {
		bad["contextTokens"] = "must be between 1024 and 10000000"
	}
	switch t.Think {
	case "", llm.ThinkOff, llm.ThinkAuto, llm.ThinkLow, llm.ThinkMedium, llm.ThinkHigh:
	default:
		bad["think"] = "must be off, auto, low, medium or high"
	}
	if t.Toolset != "" && t.Toolset != ToolsetFull && t.Toolset != ToolsetCompact {
		bad["toolset"] = "must be full or compact"
	}
	if len([]rune(t.ExtraPrompt)) > MaxExtraPrompt || strings.ContainsRune(t.ExtraPrompt, 0) {
		bad["extraPrompt"] = fmt.Sprintf("must be at most %d characters", MaxExtraPrompt)
	}
	if err := problems(bad); err != nil {
		return Tuning{}, err
	}
	empty := t.Temperature == nil && t.ContextTokens == nil && t.Think == "" && t.Toolset == "" && t.ExtraPrompt == ""
	out := t
	err := s.st.WithoutUser(ctx, func(q *db.Queries, _ pgx.Tx) error {
		if empty {
			_, err := q.DeleteModelSettings(ctx, db.DeleteModelSettingsParams{Connection: t.Connection, Model: t.Model})
			return err
		}
		p := db.UpsertModelSettingsParams{Connection: t.Connection, Model: t.Model, ExtraPrompt: t.ExtraPrompt, Now: s.clock.Now()}
		if t.Temperature != nil {
			v := float32(*t.Temperature)
			p.Temperature = &v
		}
		if t.ContextTokens != nil {
			v := int32(*t.ContextTokens) //nolint:gosec // ≤ 10,000,000
			p.ContextTokens = &v
		}
		if t.Think != "" {
			v := string(t.Think)
			p.Think = &v
		}
		if t.Toolset != "" {
			p.Toolset = &t.Toolset
		}
		r, err := q.UpsertModelSettings(ctx, p)
		if err == nil {
			out = tuningOf(&r)
		}
		return err
	})
	if err != nil {
		return Tuning{}, err
	}
	return out, s.Load(ctx)
}
