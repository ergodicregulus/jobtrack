/**
 * Load profile for the two endpoints with stated latency budgets.
 *
 * ARRIVAL RATE, NOT VIRTUAL USERS. `ramping-arrival-rate` fires a fixed number
 * of requests per second regardless of how the application is coping. With
 * `ramping-vus`, a slow application slows the generator too — each VU waits for
 * its response before sending the next — so a regression hides as reduced
 * throughput instead of appearing as latency. That is the difference between a
 * test that catches a slowdown and one that absorbs it.
 *
 * THRESHOLDS ARE THE PASS CRITERIA. They live in the script so k6 itself exits
 * non-zero; a CI step that greps output for a number is a CI step that passes
 * when the output format changes.
 *
 * The budgets come from CLAUDE.md and backend-performance.md, not from what the
 * system happens to do today. A budget derived from current behaviour cannot
 * detect a regression, only describe one.
 *
 *   make load-test              smoke, ~30s
 *   make load-test PROFILE=full full ramp
 */
import http from 'k6/http';
import { check } from 'k6';

const BASE = __ENV.BASE_URL || 'http://api:8080';
const PROFILE = __ENV.PROFILE || 'smoke';

const stages = {
  smoke: [
    { target: 10, duration: '10s' },
    { target: 10, duration: '20s' },
  ],
  full: [
    { target: 20, duration: '30s' },
    { target: 60, duration: '1m' },
    { target: 60, duration: '2m' },
    { target: 0, duration: '30s' },
  ],
};

export const options = {
  scenarios: {
    feed: {
      executor: 'ramping-arrival-rate',
      startRate: 5,
      timeUnit: '1s',
      // Headroom so the generator is never the bottleneck. If k6 cannot
      // allocate enough VUs to hold the arrival rate it says so, and that
      // message is the one case where a failure is the test's fault.
      preAllocatedVUs: 50,
      maxVUs: 200,
      stages: stages[PROFILE] || stages.smoke,
    },

    // The dashboard is per-user and hit once per visit, where the feed is paged
    // through — so a lower rate is the honest shape, not a concession. Running
    // it concurrently with the feed is deliberate: the dashboard's pathology is
    // that it is fast in isolation and collapses under contention, and a
    // scenario that has the database to itself would never show it.
    dashboard: {
      executor: 'ramping-arrival-rate',
      startRate: 2,
      timeUnit: '1s',
      preAllocatedVUs: 20,
      maxVUs: 60,
      exec: 'dashboard',
      stages: (stages[PROFILE] || stages.smoke).map((s) => ({
        ...s,
        target: Math.max(1, Math.round(s.target / 4)),
      })),
    },
  },
  thresholds: {
    // GET /v1/jobs p95 server time <= 120ms (CLAUDE.md).
    'http_req_duration{endpoint:jobs}': ['p(95)<120'],
    // GET /v1/market is a cached public aggregate; it should be far cheaper.
    'http_req_duration{endpoint:market}': ['p(95)<120'],
    // GET /v1/me/dashboard p95 <= 400ms (backend-performance.md). Four times
    // the feed's budget because it answers four questions, and it is the one
    // endpoint measured at 13,687ms under write churn — see the note on
    // `make load-test` about running this while the ingestor works.
    'http_req_duration{endpoint:dashboard}': ['p(95)<400'],
    // An error rate above 1% means the run is measuring failures, not latency.
    http_req_failed: ['rate<0.01'],
  },
};

// A spread of real filter combinations rather than one hot URL. Repeating a
// single query measures the plan cache; the feed's cost is in the predicates.
const QUERIES = [
  '/v1/jobs?limit=25',
  '/v1/jobs?limit=25&sort=comp',
  '/v1/jobs?limit=25&mode=remote',
  '/v1/jobs?limit=25&country=US&yoe=5',
  '/v1/jobs?limit=25&q=engineer',
  '/v1/jobs?limit=25&comp_min=100000&comp_disclosed_only=true',
];

/**
 * Signs in once and hands the session to every VU.
 *
 * A seeded account rather than a fresh registration, because a new user has no
 * resume and no scores, and their dashboard is a handful of empty sections that
 * returns in single-digit milliseconds. That number would pass the budget while
 * measuring nothing — the cost here is entirely in ranking a real user's scores
 * against a real corpus. `senior@jobtrack.local` carries ~8,300 of them.
 *
 * The cookie is read back by whatever name the server chose rather than
 * hardcoded: it is `__Host-` prefixed when cookies are Secure and bare when they
 * are not, so pinning the name would break this the moment it ran against TLS.
 */
export function setup() {
  const res = http.post(
    `${BASE}/v1/auth/login`,
    JSON.stringify({ email: 'senior@jobtrack.local', password: 'dev-password-please' }),
    { headers: { 'Content-Type': 'application/json' } }
  );
  if (res.status !== 200) {
    // Fail loudly. A silent fallback to anonymous would turn every dashboard
    // request into a 401 — fast, under budget, and meaningless.
    throw new Error(
      `load test cannot sign in (${res.status}); run \`make seed\` for the demo accounts`
    );
  }
  const name = Object.keys(res.cookies).find((k) => res.cookies[k][0].value);
  return { cookie: `${name}=${res.cookies[name][0].value}` };
}

export function dashboard(data) {
  const res = http.get(`${BASE}/v1/me/dashboard`, {
    headers: { Cookie: data.cookie },
    tags: { endpoint: 'dashboard' },
  });
  check(res, { 'dashboard 200': (r) => r.status === 200 });
}

export default function () {
  const path = QUERIES[Math.floor(Math.random() * QUERIES.length)];
  const jobs = http.get(`${BASE}${path}`, { tags: { endpoint: 'jobs' } });
  check(jobs, { 'jobs 200': (r) => r.status === 200 });

  // One in five iterations also hits the public aggregate, roughly the ratio a
  // landing-page visit bears to a feed page.
  if (Math.random() < 0.2) {
    const market = http.get(`${BASE}/v1/market`, { tags: { endpoint: 'market' } });
    check(market, { 'market 200': (r) => r.status === 200 });
  }
}
