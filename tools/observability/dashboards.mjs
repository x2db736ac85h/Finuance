// Writes the Grafana dashboards of deploy/k8s/observability/files/dashboards (PLAN 17.3).
// `node tools/observability/dashboards.mjs` regenerates them; tools/check-observability.sh fails
// when the committed JSON differs. Every query selects a metric with braces (`name{…}`), so the
// metric-name check can find it. Variables: the Prometheus data source and the namespace.
import { mkdirSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

// DASHBOARDS_OUT: write elsewhere (the drift check compares that with the committed files).
const out =
  process.env.DASHBOARDS_OUT ??
  join(dirname(fileURLToPath(import.meta.url)), '..', '..', 'deploy', 'k8s', 'observability', 'files', 'dashboards');
const NS = 'namespace="$namespace"';
const API = `job="finuance-api",${NS}`;
const WORKER = `job="finuance-worker",${NS}`;
const DB = `job="finuance-db",${NS}`;
const ds = { type: 'prometheus', uid: '${datasource}' };

let nextId = 1;
const target = (expr, legend = '', i = 0) => ({ datasource: ds, expr, legendFormat: legend, refId: String.fromCharCode(65 + i) });
const targets = (qs) => qs.map(([expr, legend], i) => target(expr, legend, i));

/** A time series panel; qs = [[expr, legend], …]. */
function ts(title, qs, { unit = 'short', w = 12, h = 8, stack = false, desc = '', min } = {}) {
  return {
    id: nextId++, type: 'timeseries', title, description: desc, datasource: ds, targets: targets(qs),
    gridPos: { w, h },
    fieldConfig: {
      defaults: { unit, ...(min === undefined ? {} : { min }), custom: { fillOpacity: stack ? 30 : 8, lineWidth: 1, stacking: { mode: stack ? 'normal' : 'none' } } },
      overrides: [],
    },
    options: { legend: { displayMode: 'table', placement: 'bottom', calcs: ['mean', 'max'] }, tooltip: { mode: 'multi', sort: 'desc' } },
  };
}

/** A single-value panel with thresholds [[value, colour], …] (the first value is null). */
function stat(title, expr, { unit = 'short', w = 6, h = 4, thresholds = [[null, 'green']], desc = '', decimals } = {}) {
  return {
    id: nextId++, type: 'stat', title, description: desc, datasource: ds, targets: [target(expr)],
    gridPos: { w, h },
    fieldConfig: {
      defaults: { unit, ...(decimals === undefined ? {} : { decimals }), thresholds: { mode: 'absolute', steps: thresholds.map(([value, color]) => ({ value, color })) } },
      overrides: [],
    },
    options: { reduceOptions: { calcs: ['lastNotNull'], fields: '', values: false }, colorMode: 'value', graphMode: 'area', textMode: 'auto' },
  };
}

/** A table of instant values, one row per series. */
function table(title, expr, { w = 24, h = 8, unit = 'short', desc = '' } = {}) {
  return {
    id: nextId++, type: 'table', title, description: desc, datasource: ds,
    targets: [{ ...target(expr), format: 'table', instant: true }],
    gridPos: { w, h }, fieldConfig: { defaults: { unit }, overrides: [] },
    options: { showHeader: true, sortBy: [{ displayName: 'Value', desc: true }] },
    transformations: [{ id: 'organize', options: { excludeByName: { Time: true, __name__: true } } }],
  };
}

const row = (title) => ({ id: nextId++, type: 'row', title, collapsed: false, gridPos: { w: 24, h: 1 }, panels: [] });

/** Places panels left to right, wrapping at 24 columns. */
function layout(panels) {
  let x = 0, y = 0, rowH = 0;
  for (const p of panels) {
    if (x + p.gridPos.w > 24 || p.type === 'row') { y += rowH; x = 0; rowH = 0; }
    p.gridPos = { ...p.gridPos, x, y };
    x += p.gridPos.w; rowH = Math.max(rowH, p.gridPos.h);
    if (p.type === 'row') { y += 1; x = 0; rowH = 0; }
  }
  return panels;
}

function dashboard(uid, title, description, panels, { tags = [] } = {}) {
  nextId = 1;
  return {
    uid, title, description, tags: ['finuance', ...tags], editable: true, schemaVersion: 39, version: 1,
    time: { from: 'now-6h', to: 'now' }, refresh: '1m', timezone: 'browser', graphTooltip: 1,
    links: [{ title: 'Finuance dashboards', type: 'dashboards', tags: ['finuance'], asDropdown: true }],
    templating: {
      list: [
        { name: 'datasource', label: 'Data source', type: 'datasource', query: 'prometheus', current: {}, hide: 0 },
        {
          name: 'namespace', label: 'Namespace', type: 'query', datasource: ds, refresh: 2, hide: 0, current: {},
          query: { query: 'label_values(finuance_build_info, namespace)', refId: 'ns' }, definition: 'label_values(finuance_build_info, namespace)',
        },
      ],
    },
    annotations: { list: [] },
    panels: layout(panels().map((p) => ({ ...p }))),
  };
}

const q95 = (metric, sel, by) => `histogram_quantile(0.95, sum by (${['le', ...by].join(', ')}) (rate(${metric}_bucket{${sel}}[5m])))`;
const q50 = (metric, sel, by) => `histogram_quantile(0.5, sum by (${['le', ...by].join(', ')}) (rate(${metric}_bucket{${sel}}[5m])))`;
// The error side may have no series at all (nothing failed yet): count that as 0, not as no data.
const ratio = (bad, all) => `(${bad} or vector(0)) / (${all})`;
const orZero = (expr) => `${expr} or vector(0)`;

const dashboards = {
  'finuance-api': dashboard('finuance-api', 'Finuance · API', 'Rate, errors and duration per route (RED). Routes are the API patterns, never raw paths.', () => [
    stat('Requests / s', `sum(rate(http_server_requests_total{${API}}[5m]))`, { unit: 'reqps' }),
    stat('5xx share (5 m)', ratio(`sum(rate(http_server_requests_total{${API},status_class="5xx"}[5m]))`, `sum(rate(http_server_requests_total{${API}}[5m]))`),
      { unit: 'percentunit', decimals: 2, thresholds: [[null, 'green'], [0.005, 'orange'], [0.02, 'red']] }),
    stat('p95 latency (all routes)', q95('http_server_request_duration_seconds', API, []), { unit: 's', thresholds: [[null, 'green'], [0.3, 'orange'], [1, 'red']] }),
    stat('In flight', `sum(http_server_requests_in_flight{${API}})`),
    row('Rate'),
    ts('Requests by route', [[`sum by (route) (rate(http_server_requests_total{${API}}[5m]))`, '{{route}}']], { unit: 'reqps', stack: true }),
    ts('Requests by status class', [[`sum by (status_class) (rate(http_server_requests_total{${API}}[5m]))`, '{{status_class}}']], { unit: 'reqps', stack: true }),
    row('Errors'),
    ts('5xx by route', [[`sum by (route) (rate(http_server_requests_total{${API},status_class="5xx"}[5m]))`, '{{route}}']], { unit: 'reqps' }),
    ts('4xx by route', [[`sum by (route) (rate(http_server_requests_total{${API},status_class="4xx"}[5m]))`, '{{route}}']], { unit: 'reqps',
      desc: '401, 404, 409, 422 and 429 are normal client answers; a sudden rise can still mean a broken web build.' }),
    row('Duration'),
    ts('p95 by route', [[q95('http_server_request_duration_seconds', API, ['route']), '{{route}}']], { unit: 's' }),
    ts('p50 by route', [[q50('http_server_request_duration_seconds', API, ['route']), '{{route}}']], { unit: 's' }),
    table('Slowest routes (p95, last hour)', `topk(15, histogram_quantile(0.95, sum by (le, route) (rate(http_server_request_duration_seconds_bucket{${API}}[1h]))))`, { unit: 's' }),
  ], { tags: ['api'] }),

  'finuance-jobs': dashboard('finuance-jobs', 'Finuance · Imports and jobs', 'River jobs on the worker, the import pipeline and its rows.', () => [
    stat('Imports committed (24 h)', `sum(increase(finuance_imports_total{${WORKER},result="committed"}[24h]))`, { decimals: 0 }),
    stat('Imports failed (24 h)', orZero(`sum(increase(finuance_imports_total{${WORKER},result="failed"}[24h]))`), { decimals: 0, thresholds: [[null, 'green'], [1, 'red']] }),
    stat('Job errors (1 h)', orZero(`sum(increase(finuance_jobs_total{${WORKER},result="error"}[1h]))`), { decimals: 0, thresholds: [[null, 'green'], [1, 'orange']] }),
    stat('p95 commit duration', `histogram_quantile(0.95, sum by (le) (rate(finuance_import_commit_duration_seconds_bucket{${WORKER}}[1h])))`, { unit: 's' }),
    row('Jobs'),
    ts('Jobs by kind and result', [[`sum by (kind, result) (rate(finuance_jobs_total{${WORKER}}[5m]))`, '{{kind}} {{result}}']], { unit: 'ops', stack: true }),
    ts('p95 job duration by kind', [[q95('finuance_job_duration_seconds', WORKER, ['kind']), '{{kind}}']], { unit: 's' }),
    row('Imports'),
    ts('Commits by result', [[`sum by (result) (rate(finuance_imports_total{${WORKER}}[15m]))`, '{{result}}']], { unit: 'ops' }),
    ts('Rows committed by kind', [[`sum by (kind) (rate(finuance_import_rows_total{${WORKER}}[15m]))`, '{{kind}}']], { unit: 'rowsps', stack: true }),
    ts('Commit duration', [
      [`histogram_quantile(0.5, sum by (le) (rate(finuance_import_commit_duration_seconds_bucket{${WORKER}}[15m])))`, 'p50'],
      [`histogram_quantile(0.95, sum by (le) (rate(finuance_import_commit_duration_seconds_bucket{${WORKER}}[15m])))`, 'p95'],
    ], { unit: 's', w: 24 }),
  ], { tags: ['worker'] }),

  'finuance-assistant': dashboard('finuance-assistant', 'Finuance · Assistant', 'Assistant runs, plan-mode fallbacks, tools, rounds, latency, tokens and model errors. No prompt or answer text is ever recorded.', () => [
    stat('Runs (24 h)', `sum(increase(finuance_assistant_runs_total{${API}}[24h]))`, { decimals: 0 }),
    stat('Plan-mode fallbacks (24 h)', `sum(increase(finuance_assistant_runs_total{${API},result="fallback_plan"}[24h]))`, { decimals: 0 }),
    stat('Model error share (1 h)', ratio(`sum(rate(finuance_assistant_model_requests_total{${API},result!="ok"}[1h]))`, `sum(rate(finuance_assistant_model_requests_total{${API}}[1h]))`),
      { unit: 'percentunit', thresholds: [[null, 'green'], [0.1, 'orange'], [0.5, 'red']] }),
    stat('p95 run duration', q95('finuance_assistant_run_duration_seconds', API, []), { unit: 's' }),
    row('Runs'),
    ts('Runs by mode and result', [[`sum by (mode, result) (rate(finuance_assistant_runs_total{${API}}[15m]))`, '{{mode}} {{result}}']], { unit: 'ops', stack: true }),
    ts('Run duration', [
      [q50('finuance_assistant_run_duration_seconds', API, ['mode']), 'p50 {{mode}}'],
      [q95('finuance_assistant_run_duration_seconds', API, ['mode']), 'p95 {{mode}}'],
    ], { unit: 's' }),
    row('Tools and model'),
    ts('Tool calls by tool and result', [[`sum by (tool, result) (rate(finuance_assistant_tool_calls_total{${API}}[15m]))`, '{{tool}} {{result}}']], { unit: 'ops', stack: true }),
    ts('Model requests by connection and result', [[`sum by (connection, result) (rate(finuance_assistant_model_requests_total{${API}}[15m]))`, '{{connection}} {{result}}']], { unit: 'ops', stack: true }),
    ts('Rounds per run', [
      [`sum(rate(finuance_assistant_rounds_sum{${API}}[1h])) / sum(rate(finuance_assistant_rounds_count{${API}}[1h]))`, 'mean'],
      [`histogram_quantile(0.95, sum by (le) (rate(finuance_assistant_rounds_bucket{${API}}[1h])))`, 'p95'],
    ]),
    ts('Tokens sent per run (approx.)', [
      [`histogram_quantile(0.5, sum by (le) (rate(finuance_assistant_tokens_sent_approx_bucket{${API}}[1h])))`, 'p50'],
      [`histogram_quantile(0.95, sum by (le) (rate(finuance_assistant_tokens_sent_approx_bucket{${API}}[1h])))`, 'p95'],
    ]),
  ], { tags: ['assistant'] }),

  'finuance-postgres': dashboard('finuance-postgres', 'Finuance · PostgreSQL', 'The CloudNativePG cluster: instances, replication, WAL archiving, backups (Barman Cloud plugin) and load.', () => [
    stat('Instances up', `sum(cnpg_collector_up{${DB}})`, { decimals: 0, thresholds: [[null, 'red'], [1, 'orange'], [2, 'green']] }),
    stat('Last backup age', `time() - max(barman_cloud_cloudnative_pg_io_last_available_backup_timestamp{${DB}})`,
      { unit: 's', thresholds: [[null, 'green'], [93600, 'red']], desc: 'The alert fires above 26 hours (daily ScheduledBackup).' }),
    stat('Recoverable back to', `time() - min(barman_cloud_cloudnative_pg_io_first_recoverability_point{${DB}})`,
      { unit: 's', desc: 'How far back point-in-time recovery reaches (the 14-day retention).' }),
    stat('Since last WAL archived', `min(cnpg_pg_stat_archiver_seconds_since_last_archival{${DB}})`,
      { unit: 's', thresholds: [[null, 'green'], [600, 'orange'], [3600, 'red']] }),
    row('Replication and WAL'),
    ts('Replication lag', [[`max by (pod) (cnpg_pg_replication_lag{${DB}})`, '{{pod}}']], { unit: 's' }),
    ts('WAL segments waiting to be archived', [[`max by (pod) (cnpg_collector_pg_wal_archive_status{${DB},value="ready"})`, '{{pod}}']]),
    ts('Archiver failures', [[`sum by (pod) (increase(cnpg_pg_stat_archiver_failed_count{${DB}}[15m]))`, '{{pod}}']]),
    ts('Backup timestamps', [
      [`max(barman_cloud_cloudnative_pg_io_last_available_backup_timestamp{${DB}}) * 1000`, 'last available'],
      [`(max(barman_cloud_cloudnative_pg_io_last_failed_backup_timestamp{${DB}}) > 0) * 1000`, 'last failed'], // absent until a backup fails (0 would pull the axis to 1970)
    ], { unit: 'dateTimeAsIso' }),
    row('Load'),
    ts('Connections', [[`sum by (pod, state) (cnpg_backends_total{${DB}})`, '{{pod}} {{state}}']], { stack: true }),
    ts('Transactions / s', [
      [`sum(rate(cnpg_pg_stat_database_xact_commit{${DB},datname="finuance"}[5m]))`, 'commit'],
      [`sum(rate(cnpg_pg_stat_database_xact_rollback{${DB},datname="finuance"}[5m]))`, 'rollback'],
    ], { unit: 'ops' }),
    ts('Cache hit ratio', [[ratio(`sum(rate(cnpg_pg_stat_database_blks_hit{${DB},datname="finuance"}[5m]))`,
      `sum(rate(cnpg_pg_stat_database_blks_hit{${DB},datname="finuance"}[5m])) + sum(rate(cnpg_pg_stat_database_blks_read{${DB},datname="finuance"}[5m]))`), 'hit ratio']],
    { unit: 'percentunit', min: 0 }),
    ts('Database size', [[`max(cnpg_pg_database_size_bytes{${DB},datname="finuance"})`, 'finuance']], { unit: 'bytes' }),
    ts('Temporary files written', [[`sum(rate(cnpg_pg_stat_database_temp_bytes{${DB},datname="finuance"}[5m]))`, 'temp bytes']], { unit: 'Bps',
      desc: 'Sorts and hashes that spill to disk (work_mem too small for a query).' }),
    ts('Deadlocks and longest transaction', [
      [`sum(increase(cnpg_pg_stat_database_deadlocks{${DB},datname="finuance"}[15m]))`, 'deadlocks (15 m)'],
      [`max(cnpg_backends_max_tx_duration_seconds{${DB}})`, 'longest transaction (s)'],
    ]),
  ], { tags: ['database'] }),

  'finuance-runtime': dashboard('finuance-runtime', 'Finuance · Go runtime and connection pool', 'The api and worker processes: CPU, memory, goroutines, GC and the pgx connection pool.', () => [
    ts('CPU', [[`sum by (job, pod) (rate(process_cpu_seconds_total{job=~"finuance-(api|worker)",${NS}}[5m]))`, '{{job}} {{pod}}']], { unit: 'cores' }),
    ts('Resident memory', [[`sum by (job, pod) (process_resident_memory_bytes{job=~"finuance-(api|worker)",${NS}})`, '{{job}} {{pod}}']], { unit: 'bytes' }),
    ts('Heap in use', [[`sum by (job, pod) (go_memstats_heap_inuse_bytes{job=~"finuance-(api|worker)",${NS}})`, '{{job}} {{pod}}']], { unit: 'bytes' }),
    ts('Goroutines', [[`sum by (job, pod) (go_goroutines{job=~"finuance-(api|worker)",${NS}})`, '{{job}} {{pod}}']]),
    ts('GC pause (p75 over the scrape)', [[`max by (job, pod) (go_gc_duration_seconds{job=~"finuance-(api|worker)",${NS},quantile="0.75"})`, '{{job}} {{pod}}']], { unit: 's' }),
    ts('Open file descriptors', [[`sum by (job, pod) (process_open_fds{job=~"finuance-(api|worker)",${NS}})`, '{{job}} {{pod}}']]),
    row('Connection pool (pgxpool)'),
    ts('Connections in use / max', [
      [`sum by (job, pod) (pgxpool_acquired_conns{job=~"finuance-(api|worker)",${NS}})`, 'in use {{job}} {{pod}}'],
      [`sum by (job, pod) (pgxpool_max_conns{job=~"finuance-(api|worker)",${NS}})`, 'max {{job}} {{pod}}'],
    ]),
    ts('Average wait for a connection', [[ratio(`sum by (job, pod) (rate(pgxpool_acquire_duration_seconds_total{job=~"finuance-(api|worker)",${NS}}[5m]))`,
      `sum by (job, pod) (rate(pgxpool_acquire_total{job=~"finuance-(api|worker)",${NS}}[5m]))`), '{{job}} {{pod}}']], { unit: 's' }),
    ts('Acquires that had to wait', [[`sum by (job, pod) (rate(pgxpool_empty_acquire_total{job=~"finuance-(api|worker)",${NS}}[5m]))`, '{{job}} {{pod}}']], { unit: 'ops' }),
    ts('Connections opened and recycled', [
      [`sum by (job) (rate(pgxpool_new_conns_total{job=~"finuance-(api|worker)",${NS}}[15m]))`, 'opened {{job}}'],
      [`sum by (job) (rate(pgxpool_max_idle_destroy_total{job=~"finuance-(api|worker)",${NS}}[15m]))`, 'idle closed {{job}}'],
    ], { unit: 'ops' }),
    table('Versions running', `finuance_build_info{job=~"finuance-(api|worker)",${NS}}`, { h: 6 }),
  ], { tags: ['runtime'] }),

  'finuance-slo': dashboard('finuance-slo', 'Finuance · SLOs', 'Availability (99.5 % non-5xx over 30 days) and latency (95 % of table and chart requests under 300 ms), their error budgets and burn rates (docs/SLO.md).', () => {
    const avail = `job="finuance-api",${NS},route!~"GET /(healthz|readyz)|.*/assistant/.*|unmatched"`;
    const lat = `job="finuance-api",${NS},route=~"POST /api/v1/(transactions/query|insights/.*)"`;
    const avail30 = `1 - (${ratio(`sum(increase(http_server_requests_total{${avail},status_class="5xx"}[30d]))`, `sum(increase(http_server_requests_total{${avail}}[30d]))`)})`;
    const lat30 = `sum(increase(http_server_request_duration_seconds_bucket{${lat},le="0.3"}[30d])) / sum(increase(http_server_request_duration_seconds_count{${lat}}[30d]))`;
    return [
      stat('Availability (30 d)', avail30, { unit: 'percentunit', decimals: 3, thresholds: [[null, 'red'], [0.995, 'green']] }),
      stat('Availability budget left', `1 - ((1 - (${avail30})) / 0.005)`, { unit: 'percentunit', decimals: 1, thresholds: [[null, 'red'], [0.25, 'orange'], [0.5, 'green']] }),
      stat('Latency SLI (30 d)', lat30, { unit: 'percentunit', decimals: 2, thresholds: [[null, 'red'], [0.95, 'green']] }),
      stat('Latency budget left', `1 - ((1 - (${lat30})) / 0.05)`, { unit: 'percentunit', decimals: 1, thresholds: [[null, 'red'], [0.25, 'orange'], [0.5, 'green']] }),
      row('Burn rates (1 = the budget lasts exactly 30 days)'),
      ts('Availability burn rate', [
        ['finuance:slo_availability:error_ratio_rate1h{} / 0.005', '1 h'],
        ['finuance:slo_availability:error_ratio_rate6h{} / 0.005', '6 h'],
        ['finuance:slo_availability:error_ratio_rate3d{} / 0.005', '3 d'],
      ], { desc: 'Pages above 14.4 (1 h and 5 m), tickets above 6 (6 h and 30 m).' }),
      ts('Latency burn rate', [
        ['finuance:slo_latency:error_ratio_rate1h{} / 0.05', '1 h'],
        ['finuance:slo_latency:error_ratio_rate6h{} / 0.05', '6 h'],
        ['finuance:slo_latency:error_ratio_rate3d{} / 0.05', '3 d'],
      ], { desc: 'Pages above 14.4 (1 h and 5 m), tickets above 6 (6 h and 30 m).' }),
      ts('Latency of the SLO routes', [
        [q95('http_server_request_duration_seconds', lat, ['route']), 'p95 {{route}}'],
      ], { unit: 's', w: 24 }),
      row('At the ingress (Traefik; ingress.traefik in the chart)'),
      ts('Requests at the ingress by status', [
        ['sum by (code) (rate(traefik_router_requests_total{job="traefik",router=~"([a-z0-9]+-)?$namespace-.+"}[5m]))', '{{code}}'],
      ], { unit: 'reqps', stack: true, desc: 'What users get, including requests the ingress refused before they reached the api (e.g. 503 when no api pod was ready). The SLOs above are measured at the api.' }),
      ts('Failed at the ingress vs at the api', [
        [ratio('sum(rate(traefik_router_requests_total{job="traefik",router=~"([a-z0-9]+-)?$namespace-.+",code=~"5.."}[5m]))', 'sum(rate(traefik_router_requests_total{job="traefik",router=~"([a-z0-9]+-)?$namespace-.+"}[5m]))'), 'ingress 5xx'],
        [ratio(`sum(rate(http_server_requests_total{${avail},status_class="5xx"}[5m]))`, `sum(rate(http_server_requests_total{${avail}}[5m]))`), 'api 5xx (SLO routes)'],
      ], { unit: 'percentunit', desc: 'A gap between the two lines is requests that never reached the api. FinuanceIngressErrors pages above 5 %.' }),
      table('Firing Finuance alerts', 'ALERTS{alertname=~"Finuance.*",alertstate="firing"}', { h: 6 }),
    ];
  }, { tags: ['slo'] }),
};

mkdirSync(out, { recursive: true });
for (const [name, d] of Object.entries(dashboards)) {
  writeFileSync(join(out, `${name}.json`), `${JSON.stringify(d, null, 2)}\n`);
}
console.log(`dashboards: ${Object.keys(dashboards).length} written to ${out}`);
