package store

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

// poolCollector exports pgxpool.Stat() as Prometheus metrics, read at scrape time.
type poolCollector struct {
	pool                                                *pgxpool.Pool
	acquired, idle, total, max                          *prometheus.Desc
	acquires, acquireSeconds, canceled, empty, newConns *prometheus.Desc
	lifetimeDestroys, idleDestroys                      *prometheus.Desc
}

// NewPoolCollector returns a collector for the pool's statistics.
func NewPoolCollector(p *pgxpool.Pool) prometheus.Collector {
	d := func(name, help string) *prometheus.Desc { return prometheus.NewDesc("pgxpool_"+name, help, nil, nil) }
	return &poolCollector{
		pool:             p,
		acquired:         d("acquired_conns", "Connections currently acquired."),
		idle:             d("idle_conns", "Idle connections."),
		total:            d("total_conns", "Total connections (acquired, idle, constructing)."),
		max:              d("max_conns", "Configured maximum pool size."),
		acquires:         d("acquire_total", "Successful acquires."),
		acquireSeconds:   d("acquire_duration_seconds_total", "Time spent acquiring connections."),
		canceled:         d("canceled_acquire_total", "Acquires cancelled by their context."),
		empty:            d("empty_acquire_total", "Acquires that had to wait for a connection."),
		newConns:         d("new_conns_total", "Connections opened."),
		lifetimeDestroys: d("max_lifetime_destroy_total", "Connections closed for exceeding MaxConnLifetime."),
		idleDestroys:     d("max_idle_destroy_total", "Connections closed for exceeding MaxConnIdleTime."),
	}
}

func (c *poolCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{c.acquired, c.idle, c.total, c.max, c.acquires, c.acquireSeconds, c.canceled, c.empty, c.newConns, c.lifetimeDestroys, c.idleDestroys} {
		ch <- d
	}
}

func (c *poolCollector) Collect(ch chan<- prometheus.Metric) {
	s := c.pool.Stat()
	g := func(d *prometheus.Desc, v float64) { ch <- prometheus.MustNewConstMetric(d, prometheus.GaugeValue, v) }
	k := func(d *prometheus.Desc, v float64) {
		ch <- prometheus.MustNewConstMetric(d, prometheus.CounterValue, v)
	}
	g(c.acquired, float64(s.AcquiredConns()))
	g(c.idle, float64(s.IdleConns()))
	g(c.total, float64(s.TotalConns()))
	g(c.max, float64(s.MaxConns()))
	k(c.acquires, float64(s.AcquireCount()))
	k(c.acquireSeconds, s.AcquireDuration().Seconds())
	k(c.canceled, float64(s.CanceledAcquireCount()))
	k(c.empty, float64(s.EmptyAcquireCount()))
	k(c.newConns, float64(s.NewConnsCount()))
	k(c.lifetimeDestroys, float64(s.MaxLifetimeDestroyCount()))
	k(c.idleDestroys, float64(s.MaxIdleDestroyCount()))
}
