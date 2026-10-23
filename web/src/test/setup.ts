// Vitest setup. jsdom lacks matchMedia and ResizeObserver; tests that care stub them themselves.
import { cleanup } from '@testing-library/react';
import { afterEach } from 'vitest';

import { useToasts } from '../ui/toastStore';

if (typeof window.matchMedia !== 'function') {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: (query: string) => ({
      matches: false,
      media: query,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
    }),
  });
}

// jsdom has no ResizeObserver: elements are measured once (useSize) and never again.
if (typeof window.ResizeObserver !== 'function') {
  window.ResizeObserver = class {
    observe() {
      return undefined;
    }
    unobserve() {
      return undefined;
    }
    disconnect() {
      return undefined;
    }
  };
}

// Without vitest globals, Testing Library can't register its own cleanup.
afterEach(() => {
  cleanup();
  useToasts.setState({ queue: [] });
});
