import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useState } from 'react';
import { RouterProvider } from 'react-router';

import { ApiError } from './api/client';
import { createRouter } from './routes';

export function newQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        // client errors (4xx) won't change on a retry
        retry: (n, e) => n < 2 && !(e instanceof ApiError && e.status < 500),
        refetchOnWindowFocus: false,
      },
    },
  });
}

export function App() {
  const [qc] = useState(newQueryClient);
  const [router] = useState(createRouter);
  return (
    <QueryClientProvider client={qc}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  );
}
