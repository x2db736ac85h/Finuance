// Renders the real routes on a memory router with a fresh query client, against the MSW server.

import { QueryClientProvider } from '@tanstack/react-query';
import { render } from '@testing-library/react';
import { createMemoryRouter, RouterProvider } from 'react-router';

import { newQueryClient } from '../App';
import { redirectToLogin, routes } from '../routes';

export function renderApp(path: string) {
  const router = createMemoryRouter(routes, { initialEntries: [path] });
  redirectToLogin(router);
  const qc = newQueryClient();
  qc.setDefaultOptions({ queries: { ...qc.getDefaultOptions().queries, retry: false } });
  const view = render(
    <QueryClientProvider client={qc}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return { ...view, router, qc };
}
