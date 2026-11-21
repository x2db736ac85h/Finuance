import { defineConfig, devices } from '@playwright/test';

const chrome = { ...devices['Desktop Chrome'] };

// E2E runs against a running stack (make up locally, compose.ci.yaml in CI).
// - e2e/demo needs a demo-mode stack (compose.demo.yaml, make test-e2e-demo); the rest needs sign-ups.
// - e2e/sim needs the lab and its simulator (make sim-up, make test-e2e-sim).
// - e2e/admin changes instance-wide settings (defaults, switches, themes), so it runs on its own
//   after every other spec has finished (project dependency), and puts the settings back.
export default defineConfig({
  testDir: 'e2e',
  timeout: 30_000,
  retries: process.env['CI'] ? 1 : 0,
  // Every browser of a run has one client address, and sign-ups and sign-ins are limited per
  // address (30 a minute with a burst of 30). Two workers stay under that; four did not on a fast
  // machine (the tests then fail at a sign-in answered with 429).
  workers: 2,
  // github: failures become annotations on the CI run (readable without the log)
  reporter: process.env['CI'] ? [['html', { open: 'never' }], ['list'], ['github']] : 'list',
  use: {
    baseURL: process.env['E2E_BASE_URL'] ?? 'http://localhost:8000',
    trace: 'retain-on-failure',
  },
  projects: process.env['E2E_SIM']
    ? // the simulator's tuning page against a running lab (make sim-up; e2e/sim)
      [
        {
          name: 'sim',
          use: { ...chrome, baseURL: process.env['E2E_SIM_URL'] ?? 'http://localhost:8090' },
          testMatch: 'sim/**/*.spec.ts',
          workers: 1,
        },
      ]
    : process.env['E2E_DEMO']
      ? [{ name: 'demo', use: chrome, testMatch: 'demo/**/*.spec.ts' }]
      : [
          { name: 'chromium', use: chrome, testIgnore: ['demo/**', 'admin/**', 'sim/**'] },
          { name: 'instance', use: chrome, testMatch: 'admin/**/*.spec.ts', dependencies: ['chromium'], workers: 1 },
        ],
});
