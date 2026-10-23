// The overview route's entry (eager, small): the page itself is a separate chunk (PLAN §7.12),
// loaded at idle time after the app starts and rendered without Suspense once it's there (React
// holds a resolved Suspense boundary back for ~300 ms). The overview request starts here, in
// parallel with loading the page, and the page reads it from the cache.

import { useEffect, useState, type ComponentType } from 'react';

import { useOverview } from '../api/hooks/overview';
import { useUi } from '../stores/uiStore';
import { useViews, viewOf } from '../stores/viewStore';
import { useOverviewQuery } from './data';

type Page = ComponentType<{ viewId: string }>;

let page: Page | null = null;
let loading: Promise<Page> | null = null;

/** Loads the overview's chunk once. */
export function loadOverview(): Promise<Page> {
  loading ??= import('./OverviewPage').then((m) => (page = m.OverviewPage));
  return loading;
}

export function Overview({ viewId }: { viewId: string }) {
  const [Loaded, setLoaded] = useState<Page | null>(() => page);
  const q = useOverviewQuery(viewId);
  const ovMonth = useViews((s) => viewOf(s.views, viewId).ovMonth);
  const months = useUi((s) => s.cfg.ovMonths);
  useOverview(q.context, q.ctxKey, ovMonth, months);
  useEffect(() => {
    if (Loaded) return;
    let live = true;
    void loadOverview().then((p) => {
      if (live) setLoaded(() => p);
    });
    return () => {
      live = false;
    };
  }, [Loaded]);
  return Loaded ? <Loaded viewId={viewId} /> : null;
}
