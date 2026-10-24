import { afterEach, describe, expect, it, vi } from 'vitest';

import { handleKey, type KeyActions } from './keys';

const actions = () =>
  ({
    focusSearch: vi.fn(() => true),
    openFiles: vi.fn(),
    toggleAssistant: vi.fn(),
    openShortcuts: vi.fn(),
    move: vi.fn<KeyActions['move']>(() => true),
    extend: vi.fn<KeyActions['extend']>(() => true),
    escape: vi.fn(() => true),
  }) satisfies KeyActions;

const key = (k: string, init: KeyboardEventInit = {}) =>
  new KeyboardEvent('keydown', { key: k, cancelable: true, ...init });
const none = { modal: 0, pop: 0 };

afterEach(() => {
  document.body.replaceChildren();
});

describe('handleKey', () => {
  it('maps the legacy shortcuts', () => {
    const a = actions();
    const o = key('o', { ctrlKey: true });
    handleKey(o, a, none);
    expect(a.openFiles).toHaveBeenCalled();
    expect(o.defaultPrevented).toBe(true);
    handleKey(key('j', { metaKey: true }), a, none);
    expect(a.toggleAssistant).toHaveBeenCalled();
    handleKey(key('F', { metaKey: true }), a, none);
    handleKey(key('/'), a, none);
    expect(a.focusSearch).toHaveBeenCalledTimes(2);
    handleKey(key('?'), a, none);
    expect(a.openShortcuts).toHaveBeenCalled();
  });

  it('leaves ⌘F to the browser when there is nothing to search', () => {
    const a = { ...actions(), focusSearch: vi.fn(() => false) };
    const e = key('f', { ctrlKey: true });
    handleKey(e, a, none);
    expect(e.defaultPrevented).toBe(false);
  });

  it('ignores everything while a modal is open', () => {
    const a = actions();
    handleKey(key('o', { ctrlKey: true }), a, { modal: 1, pop: 0 });
    handleKey(key('?'), a, { modal: 1, pop: 0 });
    expect(a.openFiles).not.toHaveBeenCalled();
    expect(a.openShortcuts).not.toHaveBeenCalled();
  });

  it('keeps ⌘ shortcuts but blocks plain keys while a popover is open', () => {
    const a = actions();
    handleKey(key('?'), a, { modal: 0, pop: 1 });
    handleKey(key('/'), a, { modal: 0, pop: 1 });
    handleKey(key('j', { ctrlKey: true }), a, { modal: 0, pop: 1 });
    expect(a.openShortcuts).not.toHaveBeenCalled();
    expect(a.focusSearch).not.toHaveBeenCalled();
    expect(a.toggleAssistant).toHaveBeenCalled();
  });

  it('does not take plain keys from form controls', () => {
    const a = actions();
    const input = document.createElement('input');
    document.body.append(input);
    input.focus();
    handleKey(key('?'), a, none);
    handleKey(key('/'), a, none);
    expect(a.openShortcuts).not.toHaveBeenCalled();
    expect(a.focusSearch).not.toHaveBeenCalled();
    handleKey(key('o', { metaKey: true }), a, none);
    expect(a.openFiles).toHaveBeenCalled();
  });
});

describe('table keys', () => {
  it('moves the selection with arrows, j/k and pages, not while typing', () => {
    const a = actions();
    for (const k of ['ArrowDown', 'j', 'ArrowUp', 'k', 'PageDown', 'PageUp']) handleKey(key(k), a, none);
    expect(a.move.mock.calls.map((c) => c[0])).toEqual([1, 1, -1, -1, 'pageDown', 'pageUp']);
    const input = document.createElement('input');
    document.body.append(input);
    input.focus();
    handleKey(key('j'), a, none);
    expect(a.move).toHaveBeenCalledTimes(6);
  });

  it('Shift+arrows and Shift+j/k extend a range instead of moving', () => {
    const a = actions();
    for (const k of ['ArrowDown', 'ArrowUp', 'J', 'K']) {
      const e = key(k, { shiftKey: true });
      handleKey(e, a, none);
      expect(e.defaultPrevented).toBe(true);
    }
    expect(a.extend.mock.calls.map((c) => c[0])).toEqual([1, -1, 1, -1]);
    expect(a.move).not.toHaveBeenCalled();
  });

  it('Esc clears the selection unless a popover is open', () => {
    const a = actions();
    handleKey(key('Escape'), a, { modal: 0, pop: 1 });
    expect(a.escape).not.toHaveBeenCalled();
    const e = key('Escape');
    handleKey(e, a, none);
    expect(a.escape).toHaveBeenCalledTimes(1);
    expect(e.defaultPrevented).toBe(true);
  });
});
