// Routes (PLAN §7.3). /__ui is the dev-only primitives gallery; import.meta.env.DEV is false in
// production builds, so Vite drops the route and its chunk.

import { createBrowserRouter, Navigate, useLocation, useParams, type RouteObject } from 'react-router';

import { loginPath, setUnauthenticatedHandler } from './api/client';
import { Login } from './auth/Login';
import { ImportRoute } from './import/ImportWindow';
import { overviewPath, sharedPath, txPath } from './lib/paths';
import { RequireAuth } from './auth/RequireAuth';
import { Signup } from './auth/Signup';
import { SettingsPage } from './settings/SettingsPage';
import { AppShell } from './shell/AppShell';
import { ScreenError } from './shell/ScreenError';
import { OverviewView, TableView } from './shell/Views';

/**
 * An address the app used before Phase 26 (`/t/…`, `/o/…`, `/s…`, a Settings section that moved):
 * old bookmarks and links keep working. The query string stays; `hash` names a part of the new page.
 */
function Moved({ to, hash }: { to: (id?: string) => string; hash?: string }) {
  const { id } = useParams();
  const location = useLocation();
  return <Navigate to={{ pathname: to(id), search: location.search, hash: hash ?? location.hash }} replace />;
}

export const routes: RouteObject[] = [
  {
    // a screen that fails to draw is reported and replaced by a way back (PLAN 24.8)
    errorElement: <ScreenError />,
    children: [
      { path: '/login', element: <Login /> },
      { path: '/signup', element: <Signup /> },
      ...(import.meta.env.DEV
        ? [{ path: '/__ui', lazy: async () => ({ Component: (await import('./ui/Gallery')).Gallery }) }]
        : []),
      {
        element: <RequireAuth>{(user) => <AppShell user={user} />}</RequireAuth>,
        children: [
          // the transactions are the first page, whatever was open last (the maintainer, 2026-11-25)
          { index: true, element: <Navigate to={txPath()} replace /> },
          { path: '/transactions/:viewId', element: <TableView /> },
          { path: '/overview/:viewId', element: <OverviewView /> },
          { path: '/t/:id', element: <Moved to={txPath} /> },
          { path: '/o/:id', element: <Moved to={overviewPath} /> },
          { path: '/s', element: <Moved to={sharedPath} /> },
          { path: '/s/:id', element: <Moved to={sharedPath} /> },
          { path: '/imports/:importId', element: <ImportRoute /> },
          // the Shared tab is its own chunk (Phase 19, ADR 0028)
          { path: '/shared', lazy: async () => ({ Component: (await import('./shared/SharedPage')).SharedPage }) },
          {
            path: '/shared/:groupId',
            lazy: async () => ({ Component: (await import('./shared/SharedPage')).SharedPage }),
          },
          { path: '/settings', element: <Navigate to="/settings/general" replace /> },
          // friends live on the Shared page since Phase 26 (U8)
          { path: '/settings/friends', element: <Moved to={sharedPath} hash="#friends" /> },
          { path: '/settings/:section', element: <SettingsPage /> },
          { path: '/admin', element: <Navigate to="/admin/users" replace /> },
          // the admin panel is its own chunk (Phase 19)
          { path: '/admin/:section', lazy: async () => ({ Component: (await import('./admin/AdminPage')).AdminPage }) },
          { path: '*', element: <Navigate to="/" replace /> },
        ],
      },
    ],
  },
];

/** A 401 anywhere: client-side navigation to the sign-in page, coming back afterwards. */
export function redirectToLogin(router: ReturnType<typeof createBrowserRouter>) {
  setUnauthenticatedHandler(() => {
    const { pathname, search } = router.state.location;
    const to = loginPath(pathname + search);
    if (to) void router.navigate(to, { replace: true });
  });
}

export function createRouter() {
  const router = createBrowserRouter(routes);
  redirectToLogin(router);
  return router;
}
