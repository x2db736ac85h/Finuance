// Routes (PLAN §7.3). /__ui is the dev-only primitives gallery; import.meta.env.DEV is false in
// production builds, so Vite drops the route and its chunk.

import { createBrowserRouter, Navigate, type RouteObject } from 'react-router';

import { loginPath, setUnauthenticatedHandler } from './api/client';
import { Login } from './auth/Login';
import { ImportRoute } from './import/ImportWindow';
import { RequireAuth } from './auth/RequireAuth';
import { Signup } from './auth/Signup';
import { SettingsPage } from './settings/SettingsPage';
import { AppShell } from './shell/AppShell';
import { OverviewView, TableView } from './shell/Views';
import { useUi } from './stores/uiStore';

function Start() {
  return <Navigate to={useUi.getState().mode === 'ov' ? '/o/all' : '/t/all'} replace />;
}

export const routes: RouteObject[] = [
  { path: '/login', element: <Login /> },
  { path: '/signup', element: <Signup /> },
  ...(import.meta.env.DEV
    ? [{ path: '/__ui', lazy: async () => ({ Component: (await import('./ui/Gallery')).Gallery }) }]
    : []),
  {
    element: <RequireAuth>{(user) => <AppShell user={user} />}</RequireAuth>,
    children: [
      { index: true, element: <Start /> },
      { path: '/t/:viewId', element: <TableView /> },
      { path: '/o/:viewId', element: <OverviewView /> },
      { path: '/imports/:importId', element: <ImportRoute /> },
      { path: '/settings', element: <Navigate to="/settings/appearance" replace /> },
      { path: '/settings/:section', element: <SettingsPage /> },
      { path: '*', element: <Navigate to="/" replace /> },
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
