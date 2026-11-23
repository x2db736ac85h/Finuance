package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/sim/control/gen"
)

// What the page's live strip shows (PLAN 25.16, ADR 0044): fixed queries against the lab's
// Prometheus, the last resourceWindow at resourceStep, for every viewer from one cache.
const (
	resourceWindow = 15 * time.Minute
	resourceStep   = 15 * time.Second
	resourceCache  = 5 * time.Second
)

// namespaceRe is a lab's namespace as Prometheus labels it: a Kubernetes name, so it goes into a
// query as it is.
var namespaceRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// resources reads the lab's figures from Prometheus.
type resources struct {
	url       string // Prometheus's address ("" for none)
	namespace string // the lab's namespace (finuance on Compose)
	client    *http.Client
	now       func() time.Time

	mu      sync.Mutex
	at      time.Time
	last    gen.Resources
	pending chan struct{} // one query at a time; the others wait for its answer
}

func newResources(prometheusURL, namespace string, now func() time.Time) (*resources, error) {
	if namespace == "" {
		namespace = "finuance"
	}
	if !namespaceRe.MatchString(namespace) {
		return nil, fmt.Errorf("the lab's namespace %q is not a Kubernetes name", namespace)
	}
	if prometheusURL != "" {
		u, err := url.Parse(prometheusURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
			return nil, fmt.Errorf("the Prometheus address must be http or https without a user, not %q", prometheusURL)
		}
	}
	return &resources{url: prometheusURL, namespace: namespace, client: &http.Client{Timeout: 5 * time.Second}, now: now,
		pending: make(chan struct{}, 1)}, nil
}

// queries are the strip's figures; containers: CPU and memory by container.
func (r *resources) queries() (cpu, memory string, series map[gen.ResourceSeriesName]string) {
	ct := fmt.Sprintf(`namespace=~"%s|finuance-sim",container!="",container!="POD"`, r.namespace)
	api := fmt.Sprintf(`job="finuance-api",namespace=%q`, r.namespace)
	return fmt.Sprintf(`sum by (container) (rate(container_cpu_usage_seconds_total{%s}[1m]))`, ct),
		fmt.Sprintf(`sum by (container) (container_memory_working_set_bytes{%s})`, ct),
		map[gen.ResourceSeriesName]string{
			gen.Actions:      `sum(rate(finuance_sim_actions_total{job="finuance-sim"}[1m]))`,
			gen.Requests:     fmt.Sprintf(`sum(rate(http_server_requests_total{%s}[1m]))`, api),
			gen.P95:          fmt.Sprintf(`histogram_quantile(0.95, sum by (le) (rate(http_server_request_duration_seconds_bucket{%s}[1m])))`, api),
			gen.ServerErrors: fmt.Sprintf(`sum(rate(http_server_requests_total{%s,status_class="5xx"}[1m])) or vector(0)`, api),
		}
}

// get answers the figures, from the cache when they are fresh.
func (r *resources) get(ctx context.Context) gen.Resources {
	if r.url == "" {
		return gen.Resources{Available: false, Error: ptr("the simulator was started without -prometheus"), Containers: []gen.ContainerUsage{},
			Series: []gen.ResourceSeries{}}
	}
	select {
	case r.pending <- struct{}{}:
		defer func() { <-r.pending }()
	case <-ctx.Done():
		return gen.Resources{Available: false, Error: ptr(ctx.Err().Error()), Containers: []gen.ContainerUsage{}, Series: []gen.ResourceSeries{}}
	}
	r.mu.Lock()
	if !r.at.IsZero() && r.now().Sub(r.at) < resourceCache {
		out := r.last
		r.mu.Unlock()
		return out
	}
	r.mu.Unlock()
	out := r.fetch(ctx)
	r.mu.Lock()
	r.at, r.last = r.now(), out
	r.mu.Unlock()
	return out
}

func (r *resources) fetch(ctx context.Context) gen.Resources {
	end := r.now().Truncate(resourceStep)
	start := end.Add(-resourceWindow)
	cpuQ, memQ, seriesQ := r.queries()
	fail := func(err error) gen.Resources {
		return gen.Resources{Available: false, Error: ptr(err.Error()), Containers: []gen.ContainerUsage{}, Series: []gen.ResourceSeries{}}
	}
	cpu, err := r.rangeQuery(ctx, cpuQ, start, end)
	if err != nil {
		return fail(err)
	}
	mem, err := r.rangeQuery(ctx, memQ, start, end)
	if err != nil {
		return fail(err)
	}
	out := gen.Resources{Available: true, Containers: []gen.ContainerUsage{}, Series: []gen.ResourceSeries{}}
	byName := map[string]int{}
	index := func(name string) int {
		if i, ok := byName[name]; ok {
			return i
		}
		out.Containers = append(out.Containers, gen.ContainerUsage{Name: name, Cpu: []gen.Point{}, MemoryBytes: []gen.Point{}})
		byName[name] = len(out.Containers) - 1
		return len(out.Containers) - 1
	}
	for _, s := range cpu {
		out.Containers[index(s.labels["container"])].Cpu = s.points
	}
	for _, s := range mem {
		out.Containers[index(s.labels["container"])].MemoryBytes = s.points
	}
	slices.SortFunc(out.Containers, func(a, b gen.ContainerUsage) int { return compare(lastOf(b.MemoryBytes), lastOf(a.MemoryBytes)) })
	for _, name := range []gen.ResourceSeriesName{gen.Actions, gen.Requests, gen.P95, gen.ServerErrors} {
		got, err := r.rangeQuery(ctx, seriesQ[name], start, end)
		if err != nil {
			return fail(err)
		}
		points := []gen.Point{}
		if len(got) > 0 {
			points = got[0].points
		}
		out.Series = append(out.Series, gen.ResourceSeries{Name: name, Points: points})
	}
	return out
}

type promSeries struct {
	labels map[string]string
	points []gen.Point
}

// rangeQuery runs one query_range and reads its matrix; NaN and infinite values are left out.
func (r *resources) rangeQuery(ctx context.Context, q string, start, end time.Time) ([]promSeries, error) {
	v := url.Values{"query": {q}, "start": {strconv.FormatInt(start.Unix(), 10)}, "end": {strconv.FormatInt(end.Unix(), 10)},
		"step": {strconv.Itoa(int(resourceStep / time.Second))}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.url+"/api/v1/query_range?"+v.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, errors.New("the lab's Prometheus does not answer")
	}
	defer func() { _ = resp.Body.Close() }()
	var body struct {
		Status string `json:"status"`
		Data   struct {
			Result []struct {
				Metric map[string]string `json:"metric"`
				Values [][2]any          `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 4<<20)).Decode(&body) != nil || body.Status != "success" {
		return nil, fmt.Errorf("the lab's Prometheus answered HTTP %d", resp.StatusCode)
	}
	out := make([]promSeries, 0, len(body.Data.Result))
	for _, res := range body.Data.Result {
		s := promSeries{labels: res.Metric, points: []gen.Point{}}
		for _, pair := range res.Values {
			t, ok1 := pair[0].(float64)
			str, ok2 := pair[1].(string)
			f, err := strconv.ParseFloat(str, 64)
			if !ok1 || !ok2 || err != nil || f != f || f > 1e300 || f < -1e300 { // f != f: NaN
				continue
			}
			s.points = append(s.points, gen.Point{T: int64(t), V: f})
		}
		out = append(out, s)
	}
	return out, nil
}

func lastOf(ps []gen.Point) float64 {
	if len(ps) == 0 {
		return 0
	}
	return ps[len(ps)-1].V
}

func compare(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func ptr[T any](v T) *T { return &v }
