import { afterEach, describe, expect, it, vi } from 'vitest';

import { resolveTheme, watchTheme } from './theme';

function fakeMatchMedia(initial: boolean) {
  const listeners = new Set<() => void>();
  const mq = {
    matches: initial,
    addEventListener: (_: string, l: () => void) => listeners.add(l),
    removeEventListener: (_: string, l: () => void) => listeners.delete(l),
  };
  vi.stubGlobal('matchMedia', () => mq);
  return {
    set(v: boolean) {
      mq.matches = v;
      listeners.forEach((l) => {
        l();
      });
    },
    listeners,
  };
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('theme', () => {
  it('resolves the preference', () => {
    expect(resolveTheme('system', true)).toBe('light');
    expect(resolveTheme('system', false)).toBe('dark');
    expect(resolveTheme('dark', true)).toBe('dark');
    expect(resolveTheme('light', false)).toBe('light');
  });

  it('follows the OS only while the preference is system', () => {
    const os = fakeMatchMedia(false);
    const root = document.createElement('html');
    const stop = watchTheme('system', root);
    expect(root.dataset['theme']).toBe('dark');
    os.set(true);
    expect(root.dataset['theme']).toBe('light');
    stop();
    expect(os.listeners.size).toBe(0);

    const stopDark = watchTheme('dark', root);
    os.set(true);
    expect(root.dataset['theme']).toBe('dark');
    stopDark();
  });
});
