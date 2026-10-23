import { defineConfig, devices } from '@playwright/test';

// E2E runs against a running stack (make up locally, compose.ci.yaml in CI).
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
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
});
