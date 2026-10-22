// Vitest setup. jsdom lacks matchMedia; tests that care stub it themselves.
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

// Without vitest globals, Testing Library can't register its own cleanup.
afterEach(() => {
  cleanup();
  useToasts.setState({ queue: [] });
});
