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
  },
  thresholds: {
    // GET /v1/jobs p95 server time <= 120ms (CLAUDE.md).
    'http_req_duration{endpoint:jobs}': ['p(95)<120'],
    // GET /v1/market is a cached public aggregate; it should be far cheaper.
    'http_req_duration{endpoint:market}': ['p(95)<120'],
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
