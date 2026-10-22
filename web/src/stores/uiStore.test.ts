import { beforeEach, describe, expect, it } from 'vitest';

import { collectUi, sidebarShown, uiDefaults, useUi } from './uiStore';
import { collectViews, useViews } from './viewStore';

const ui = () => useUi.getState();

beforeEach(() => {
  useUi.setState(uiDefaults);
  useViews.getState().reset();
});

describe('sidebar', () => {
  it('is hidden by default (v7)', () => {
    expect(sidebarShown(ui())).toBe(false);
  });

  it('collapses on a narrow window and comes back when it widens (legacy sbAuto)', () => {
    ui().toggleSidebar();
    expect(sidebarShown(ui())).toBe(true);
    ui().setNarrow(true);
    expect(sidebarShown(ui())).toBe(false);
    expect(ui().sidebarOpen).toBe(true); // the user's choice is kept
    ui().setNarrow(false);
    expect(sidebarShown(ui())).toBe(true);
  });

  it('stays closed on widening when the user closed it', () => {
    ui().setNarrow(true);
    ui().setNarrow(false);
    expect(sidebarShown(ui())).toBe(false);
  });

  it('opening by hand on a narrow window forgets the auto-collapse', () => {
    ui().toggleSidebar();
    ui().setNarrow(true);
    ui().toggleSidebar();
    expect(sidebarShown(ui())).toBe(true);
    ui().setNarrow(false);
    expect(sidebarShown(ui())).toBe(true);
    ui().toggleSidebar();
    expect(ui().sidebarOpen).toBe(false);
  });
});

describe('hydrate', () => {
  it('applies stored values and keeps defaults for missing ones', () => {
    ui().hydrate({ theme: 'light', ui: 1.2, sbOpen: true, noViz: true, cfg: { payeeTop: 10 } }, false);
    const s = ui();
    expect(s.themePref).toBe('light');
    expect(s.uiScalePref).toBe(1.2);
    expect(sidebarShown(s)).toBe(true);
    expect(s.vizOpen).toBe(false);
    expect(s.cfg).toEqual({ ...uiDefaults.cfg, payeeTop: 10 });
    expect(s.ai).toEqual(uiDefaults.ai);
  });

  it('starts a saved open sidebar auto-collapsed on a narrow window', () => {
    ui().hydrate({ sbOpen: true }, true);
    expect(sidebarShown(ui())).toBe(false);
    ui().setNarrow(false);
    expect(sidebarShown(ui())).toBe(true);
  });

  it('round-trips through collectUi', () => {
    ui().hydrate({ theme: 'dark', ui: 'auto', mode: 'ov', cardOrder: ['heat'], importFastPath: false }, false);
    const doc = collectUi(ui());
    expect(doc).toMatchObject({ theme: 'dark', ui: 'auto', mode: 'ov', cardOrder: ['heat'], importFastPath: false });
    expect(doc).not.toHaveProperty('keysOpen');
    expect(doc).not.toHaveProperty('sidebarAutoCollapsed');
  });

  it('toggleViz from the overview returns to the table with the pane shown (legacy)', () => {
    ui().hydrate({ mode: 'ov', noViz: true }, false);
    ui().toggleViz();
    expect(ui().mode).toBe('table');
    expect(ui().vizOpen).toBe(true);
    ui().toggleViz();
    expect(ui().vizOpen).toBe(false);
  });
});

describe('viewStore', () => {
  it('persists only the legacy view fields', () => {
    useViews.getState().patch('all', {
      search: 'kino',
      filters: { payee: { cond: 'x', excluded: [] } },
      selectedId: 't1',
      hidden: ['note'],
      period: 'l3m',
    });
    expect(collectViews(useViews.getState().views)).toEqual({
      all: { hidden: ['note'], widths: {}, statColumn: 'amount', sort: null, period: 'l3m', custom: null },
    });
  });

  it('hydrates stored fields and keeps the session-only ones', () => {
    useViews.getState().patch('all', { search: 'kino' });
    useViews.getState().hydrate({ views: { all: { period: 'ytd' }, a1: { hidden: ['x'] } } });
    const v = useViews.getState().views;
    expect(v['all']).toMatchObject({ search: 'kino', period: 'ytd' });
    expect(v['a1']).toMatchObject({ hidden: ['x'], search: '', period: 'all' });
  });
});
