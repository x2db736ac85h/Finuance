// The ChartEnv of a view at a size: chart settings from the UI store, the view's period and filters
// (highlighting, toggles), and the chart → table actions.

import { useMemo } from 'react';

import type { ChartEnv } from '../charts/types';
import { useUi } from '../stores/uiStore';
import { useViews, viewOf } from '../stores/viewStore';
import { useChartActions } from './useChartActions';

export function useChartEnv(viewId: string, w: number, h: number, big: boolean, ready = true): ChartEnv {
  const cfg = useUi((s) => s.cfg);
  const catMode = useUi((s) => s.catMode);
  const period = useViews((s) => viewOf(s.views, viewId).period);
  const filters = useViews((s) => viewOf(s.views, viewId).filters);
  const ovMonth = useViews((s) => viewOf(s.views, viewId).ovMonth);
  const actions = useChartActions(viewId);
  return useMemo(
    () => ({ viewId, w, h, big, cfg, catMode, period, filters, ovMonth, actions, ready }),
    [viewId, w, h, big, cfg, catMode, period, filters, ovMonth, actions, ready],
  );
}
