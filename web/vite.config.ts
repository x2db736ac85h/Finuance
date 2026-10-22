import react from '@vitejs/plugin-react';
import { defineConfig } from 'vitest/config';

// The dev server proxies the API so the app runs same-origin, as behind nginx in production.
export default defineConfig(({ mode }) => ({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: { '/api': { target: 'http://localhost:8080', changeOrigin: false } },
    // Parity tests read legacy/finuance.html (?raw); only test runs may read outside web/.
    ...(mode === 'test' ? { fs: { allow: ['..'] } } : {}),
  },
  build: { sourcemap: true },
  test: {
    environment: 'jsdom',
    setupFiles: ['src/test/setup.ts'],
    include: ['src/**/*.test.{ts,tsx}'],
    css: false,
  },
}));
