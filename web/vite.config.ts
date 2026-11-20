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
  // Maps are written for whoever builds locally, but the files don't point at them: the image
  // deletes the maps, and a comment naming a missing file is a 404 on every page load.
  build: { sourcemap: 'hidden' },
  test: {
    environment: 'jsdom',
    setupFiles: ['src/test/setup.ts'],
    include: ['src/**/*.test.{ts,tsx}'],
    css: false,
  },
}));
