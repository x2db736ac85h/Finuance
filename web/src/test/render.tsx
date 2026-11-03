// Renders the real routes on a memory router with a fresh query client, against the MSW server.

import { QueryClientProvider } from '@tanstack/react-query';
import { cleanup, render, screen } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { createMemoryRouter, RouterProvider } from 'react-router';

import { newQueryClient } from '../App';
import { useChat } from '../assistant/chatStore';
import { redirectToLogin, routes } from '../routes';
import { usePrefsReady } from '../stores/persist';
import { useSelection } from '../stores/selectionStore';
import { useUi } from '../stores/uiStore';
import { useViews } from '../stores/viewStore';
import { useLayers } from '../ui/layers';
import { useToasts } from '../ui/toastStore';
import { account, API, db, resetDb, server, txRow } from './server';

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

const stores = [useUi, useViews, useSelection, usePrefsReady, useLayers, useToasts, useChat] as const;

// The first full render in a test file is much slower than the next ones (cold component code,
// first React, router and query setup, first layout). Under a busy parallel run (e.g. with a local
// cluster on the same CPU) it can miss the first test's findBy, so files whose first test renders
// the app call this in beforeAll (after their own jsdom stubs): one render of the table view with
// a row, waited for until its list has loaded, unmounted, and the data, the handlers and every
// store put back as they were. No test sees it.
export async function warmUpApp(): Promise<void> {
  const saved = stores.map((s) => s.getState());
  resetDb();
  db.accounts = [account({ txCount: 1 })];
  db.transactions = [txRow(1, { payee: 'Warm-up row' })];
  // the shared handlers leave the category list to the files that use it
  server.use(http.get(`${API}/categories`, () => HttpResponse.json([])));
  renderApp('/t/all');
  // the toolbar's "rows × columns" comes from the list request, whatever the file's layout stubs
  await screen.findByText(/^1 × \d+$/, undefined, { timeout: 10_000 });
  cleanup();
  server.resetHandlers();
  resetDb();
  stores.forEach((s, i) => {
    (s.setState as (state: unknown, replace: true) => void)(saved[i], true);
  });
}
