import { defineConfig, devices } from '@playwright/test';

const chrome = { ...devices['Desktop Chrome'] };

// E2E runs against a running stack (make up locally, compose.ci.yaml in CI).
// - e2e/demo needs a demo-mode stack (compose.demo.yaml, make test-e2e-demo); the rest needs sign-ups.
// - e2e/admin changes instance-wide settings (defaults, switches, themes), so it runs on its own
//   after every other spec has finished (project dependency), and puts the settings back.
export default defineConfig({
  testDir: 'e2e',
  timeout: 30_000,
  retries: process.env['CI'] ? 1 : 0,
  // github: failures become annotations on the CI run (readable without the log)
  reporter: process.env['CI'] ? [['html', { open: 'never' }], ['list'], ['github']] : 'list',
  use: {
    baseURL: process.env['E2E_BASE_URL'] ?? 'http://localhost:8000',
    trace: 'retain-on-failure',
  },
  projects: process.env['E2E_DEMO']
    ? [{ name: 'demo', use: chrome, testMatch: 'demo/**/*.spec.ts' }]
    : [
        { name: 'chromium', use: chrome, testIgnore: ['demo/**', 'admin/**'] },
        { name: 'instance', use: chrome, testMatch: 'admin/**/*.spec.ts', dependencies: ['chromium'], workers: 1 },
      ],
});
