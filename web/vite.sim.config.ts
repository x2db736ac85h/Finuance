import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

// The simulator's tuning page (PLAN 25.6): its own entry and bundle, embedded in the finuance-sim
// image. It reuses the app's styles and UI pieces from ../src. The dev server proxies the control
// API of a simulator running on :8090.
export default defineConfig({
  root: 'sim',
  plugins: [react()],
  server: { port: 5174, proxy: { '/api': { target: 'http://localhost:8090', changeOrigin: false } } },
  build: { outDir: '../dist-sim', emptyOutDir: true, sourcemap: 'hidden' },
});
