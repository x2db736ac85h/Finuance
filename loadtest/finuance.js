// Load test of the Finuance API (PLAN 17.5): the requests a browsing user makes — table windows
// with random filters, sorts and scrolling, the six insight charts, the overview — plus imports of
// a 5,000-row file. Thresholds follow docs/SLO.md. Run with `make loadtest` (k6 in Docker).
//
// Users: `finuance seed-demo -email=loadtest@example.test -users=N -rows=20000 -password-stdin`
// (make loadtest-seed). Every virtual user signs in as one of them in setup().
//
// Environment:
//   BASE_URL   the app (default http://localhost:8000; the web container proxies /api)
//   USERS      how many seeded users to use (default 50), EMAIL their base address, PASSWORD
//   PROFILE    load (default: steady load for 5 minutes), smoke (1 minute, a few users, for the
//              weekly workflow) or ramp (more and more browsing until it breaks)
import http from 'k6/http';
import { check, fail, sleep } from 'k6';
import exec from 'k6/execution';
import { Counter } from 'k6/metrics';

const BASE = (__ENV.BASE_URL || 'http://localhost:8000') + '/api/v1';
const USERS = Number(__ENV.USERS || 50);
const EMAIL = __ENV.EMAIL || 'loadtest@example.test';
const PASSWORD = __ENV.PASSWORD || 'load test password';
const PROFILE = __ENV.PROFILE || 'load';
const ORIGIN = __ENV.BASE_URL || 'http://localhost:8000';

const imported = new Counter('finuance_rows_imported');

const profiles = {
  smoke: {
    browse: { executor: 'constant-vus', vus: 3, duration: '1m', exec: 'browse' },
    charts: { executor: 'constant-vus', vus: 2, duration: '1m', exec: 'charts' },
    overview: { executor: 'constant-vus', vus: 1, duration: '1m', exec: 'overview' },
    imports: { executor: 'shared-iterations', vus: 1, iterations: 1, maxDuration: '1m', exec: 'importFile' },
  },
  load: {
    // About 50 people using the app at the same time, each acting every few seconds.
    browse: { executor: 'constant-vus', vus: 30, duration: '5m', exec: 'browse' },
    charts: { executor: 'constant-vus', vus: 15, duration: '5m', exec: 'charts' },
    overview: { executor: 'constant-vus', vus: 5, duration: '5m', exec: 'overview' },
    imports: { executor: 'constant-arrival-rate', rate: 2, timeUnit: '1m', duration: '5m', preAllocatedVUs: 2, exec: 'importFile' },
  },
  ramp: {
    // Browsing without think time, more and more of it, to find where the SLO breaks.
    browse: {
      executor: 'ramping-arrival-rate', startRate: 10, timeUnit: '1s', preAllocatedVUs: 50, maxVUs: 400, exec: 'browseOnce',
      stages: [{ target: 20, duration: '1m' }, { target: 40, duration: '1m' }, { target: 80, duration: '1m' }, { target: 160, duration: '1m' }, { target: 320, duration: '1m' }],
    },
  },
};

export const options = {
  setupTimeout: '10m',
  scenarios: profiles[PROFILE],
  // The SLOs: 95 % of table and chart requests under 300 ms, under 0.5 % failed requests.
  thresholds: {
    'http_req_duration{slo:latency}': ['p(95)<300'],
    'http_req_failed{slo:latency}': ['rate<0.005'],
    'checks': ['rate>0.995'],
  },
  summaryTrendStats: ['avg', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
};

const headers = (session) => ({
  'Content-Type': 'application/json',
  'X-Finuance-Client': 'web',
  Origin: ORIGIN,
  Cookie: `finuance_session=${session}`,
});

// setup signs in every load-test user once (paced under the sign-in limit of 30 per minute and
// address) and reads their account and data range.
export function setup() {
  const users = [];
  for (let i = 1; i <= USERS; i++) {
    const [local, domain] = EMAIL.split('@');
    const email = `${local}+${String(i).padStart(3, '0')}@${domain}`;
    // A jar of its own: a sign-in rotates (revokes) the session cookie the client sends along,
    // which would be the previous user's in a shared jar.
    const res = http.post(`${BASE}/auth/login`, JSON.stringify({ email, password: PASSWORD }), {
      headers: { 'Content-Type': 'application/json', 'X-Finuance-Client': 'web', Origin: ORIGIN },
      jar: new http.CookieJar(),
    });
    const cookie = res.cookies.finuance_session;
    if (res.status !== 200 || !cookie) fail(`sign-in of ${email}: ${res.status} ${res.body}`);
    const session = cookie[0].value;
    const accounts = http.get(`${BASE}/accounts`, { headers: headers(session) }).json();
    if (!accounts.length) fail(`${email} has no account; run make loadtest-seed`);
    users.push({ session, accountId: accounts[0].id, lastOn: accounts[0].lastOn });
    sleep(2.1);
  }
  return { users };
}

const pick = (a) => a[Math.floor(Math.random() * a.length)];
const userOf = (data) => data.users[(exec.vu.idInTest - 1) % data.users.length];

const SEARCHES = ['', '', '', 'warszawa', 'kawiarnia', 'apteka', 'blik'];
const FILTERS = [
  [],
  [],
  [{ column: 'amount', cond: '<-100' }],
  [{ column: 'category', cond: '=Transport' }],
  [{ column: 'payee', cond: 'market' }],
  [{ column: 'amount', cond: '>0' }],
  [{ column: 'category', cond: '!=Artykuły spożywcze' }],
];
const SORTS = [undefined, { column: 'amount', dir: 'asc' }, { column: 'bookedOn', dir: 'desc' }, { column: 'payee', dir: 'asc' }];
const PERIODS = [undefined, undefined, { preset: 'l3m' }, { preset: 'l12m' }, { preset: 'ytd' }];

function context(u) {
  const c = { accountIds: Math.random() < 0.5 ? [u.accountId] : [] };
  const search = pick(SEARCHES);
  if (search) c.search = search;
  const filters = pick(FILTERS);
  if (filters.length) c.filters = filters;
  const period = pick(PERIODS);
  if (period) c.period = period;
  return c;
}

const post = (u, path, body, tags) =>
  http.post(`${BASE}${path}`, JSON.stringify(body), { headers: headers(u.session), tags: { name: path, ...tags } });

// One table view: the first window with totals, then a few scrolled windows without.
export function browseOnce(data) {
  const u = userOf(data);
  const ctx = context(u);
  const sort = pick(SORTS);
  const first = post(u, '/transactions/query', { context: ctx, sort, limit: 200 }, { slo: 'latency' });
  check(first, { 'query 200': (r) => r.status === 200 });
  const total = first.status === 200 ? first.json('total') : 0;
  for (let i = 1; i <= 2 && i * 200 < total; i++) {
    const r = post(u, '/transactions/query', { context: ctx, sort, limit: 200, offset: i * 200, includeTotals: false }, { slo: 'latency' });
    check(r, { 'window 200': (res) => res.status === 200 });
  }
}

export function browse(data) {
  browseOnce(data);
  sleep(1 + Math.random() * 3);
}

// The insight pane: all six charts for one context, sent at once as the browser does.
export function charts(data) {
  const u = userOf(data);
  const c = context(u);
  const p = { headers: headers(u.session) };
  const req = (path, body) => ['POST', `${BASE}${path}`, JSON.stringify(body), { ...p, tags: { name: path, slo: 'latency' } }];
  const res = http.batch([
    req('/insights/summary', { context: c }),
    req('/insights/categories', { context: c }),
    req('/insights/cashflow', { context: c }),
    req('/insights/pace', { context: c, compare: 'prev' }),
    req('/insights/payees', { context: c, top: 10, scope: 'period' }),
    req('/insights/daily', { context: c, range: '6m' }),
  ]);
  check(res, { 'charts 200': (rs) => rs.every((r) => r.status === 200) });
  sleep(2 + Math.random() * 4);
}

export function overview(data) {
  const u = userOf(data);
  const r = post(u, '/overview', { context: { accountIds: [u.accountId] }, months: pick([6, 12]) }, { kind: 'overview' });
  check(r, { 'overview 200': (res) => res.status === 200 });
  sleep(3 + Math.random() * 5);
}

// A 5,000-row file (unique per iteration, so it isn't a duplicate file) uploaded, committed and
// waited for, like the import window does.
function csv(n, salt) {
  const lines = ['Data księgowania;Nadawca / Odbiorca;Tytułem;Kwota operacji;Waluta;Kategoria'];
  const day = 24 * 3600 * 1000;
  const start = Date.UTC(2020, 0, 1);
  for (let i = 0; i < n; i++) {
    const d = new Date(start + (i % 1500) * day);
    const date = `${String(d.getUTCDate()).padStart(2, '0')}.${String(d.getUTCMonth() + 1).padStart(2, '0')}.${d.getUTCFullYear()}`;
    const amount = (-(1 + ((i * 7919) % 50000) / 100)).toFixed(2).replace('.', ',');
    lines.push(`${date};SKLEP ${i % 97};Zakupy ${salt} ${i};${amount};PLN;Zakupy`);
  }
  return lines.join('\n');
}

export function importFile(data) {
  const u = data.users[exec.scenario.iterationInTest % data.users.length];
  const body = csv(5000, `${exec.vu.idInTest}-${exec.scenario.iterationInTest}-${Date.now()}`);
  const up = http.post(`${BASE}/imports`, { file: http.file(body, 'load.csv', 'text/csv'), accountId: u.accountId }, {
    headers: { 'X-Finuance-Client': 'web', Origin: ORIGIN, Cookie: `finuance_session=${u.session}` },
    tags: { name: '/imports', kind: 'import' },
  });
  if (!check(up, { 'upload 201': (r) => r.status === 201 })) return;
  const id = up.json('import.id');
  const commit = post(u, `/imports/${id}/commit`, {}, { name: '/imports/{id}/commit', kind: 'import' });
  if (!check(commit, { 'commit 202': (r) => r.status === 202 })) return;
  for (let i = 0; i < 120; i++) {
    sleep(1);
    const s = http.get(`${BASE}/imports/${id}`, { headers: headers(u.session), tags: { name: '/imports/{id}', kind: 'import' } });
    if (s.status === 200 && s.json('status') !== 'committing') {
      if (check(s, { 'import committed': (r) => r.json('status') === 'committed' })) imported.add(5000);
      // keep the users' data the same size: the next run imports again
      http.del(`${BASE}/imports/${id}`, null, { headers: headers(u.session), tags: { name: '/imports/{id}', kind: 'import' } });
      return;
    }
  }
  check(null, { 'import committed': () => false });
}
