// Load during pod kills (PLAN task 16.6; tools/rehearsal.sh runs it while it deletes and restarts
// api and web pods). Every request goes through Traefik, as in production, and none may fail:
// rollouts are maxUnavailable 0, pods leave the endpoints before they stop (preStop), and the
// api drains in-flight requests on SIGTERM.
import http from 'k6/http';
import { check, sleep } from 'k6';

const BASE = __ENV.BASE_URL || 'https://localhost:8443';
// An existing user (instances with sign-up off, tools/homelab-drill.sh) instead of a sign-up.
const EMAIL = __ENV.K6_EMAIL;
const PASSWORD = __ENV.K6_PASSWORD;
const JSON_HEADERS = { 'Content-Type': 'application/json', 'X-Finuance-Client': 'web', Origin: BASE };

export const options = {
  // the rehearsal's throwaway CA isn't in k6's trust store (curl checks the chain in the smoke tests);
  // a real certificate is checked with VERIFY_TLS=true
  insecureSkipTLSVerify: __ENV.VERIFY_TLS !== 'true',
  scenarios: {
    steady: { executor: 'constant-vus', vus: Number(__ENV.VUS || 10), duration: __ENV.DURATION || '75s' },
  },
  thresholds: {
    http_req_failed: ['rate==0'],
    checks: ['rate==1'],
  },
  summaryTrendStats: ['avg', 'p(95)', 'p(99)', 'max'],
};

export function setup() {
  let r;
  if (EMAIL) {
    r = http.post(`${BASE}/api/v1/auth/login`, JSON.stringify({ email: EMAIL, password: PASSWORD }), {
      headers: JSON_HEADERS,
    });
    if (r.status !== 200) throw new Error(`login answered ${r.status}`);
  } else {
    const email = `k6-${Date.now()}@example.test`;
    r = http.post(`${BASE}/api/v1/auth/signup`, JSON.stringify({ email, password: 'k6 load password' }), {
      headers: JSON_HEADERS,
    });
    if (r.status !== 201) throw new Error(`signup answered ${r.status}`);
  }
  const cookie = r.cookies.finuance_session[0].value;
  const demo = http.post(`${BASE}/api/v1/demo-data`, null, {
    headers: { ...JSON_HEADERS, Cookie: `finuance_session=${cookie}` },
  });
  if (demo.status !== 201) throw new Error(`demo data answered ${demo.status}`);
  return { cookie };
}

export default function (data) {
  const params = { headers: { ...JSON_HEADERS, Cookie: `finuance_session=${data.cookie}` } };
  const [page, me, query] = http.batch([
    ['GET', `${BASE}/t/all`, null, {}],
    ['GET', `${BASE}/api/v1/me`, null, params],
    ['POST', `${BASE}/api/v1/transactions/query`, JSON.stringify({ context: {}, limit: 50 }), params],
  ]);
  check(page, { 'web 200': (r) => r.status === 200 });
  check(me, { 'me 200': (r) => r.status === 200 });
  check(query, { 'query 200 with rows': (r) => r.status === 200 && r.json('total') > 0 });
  // steady traffic rather than saturation: the probes of the kind nodes must stay healthy
  sleep(0.1);
}
