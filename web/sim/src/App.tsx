import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useMemo, useState } from 'react';

import { Button } from '../../src/ui/Button';
import { TextInput } from '../../src/ui/inputs';
import { ControlError, makeClient, readToken, saveToken } from './api';
import { Dashboard } from './Dashboard';

/** The page: the control token first (kept for this tab), then the dashboard. */
export function App() {
  const [token, setToken] = useState(readToken);
  const [refused, setRefused] = useState(false);
  const client = useMemo(() => makeClient(token), [token]);
  const queries = useMemo(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: {
            retry: (n, err) => !(err instanceof ControlError && (err.status === 401 || err.status === 403)) && n < 2,
          },
        },
      }),
    [],
  );
  queries.getQueryCache().config.onError = (err) => {
    if (err instanceof ControlError && err.status === 401) {
      saveToken('');
      setRefused(true);
      setToken('');
    }
  };
  if (!token) {
    return (
      <TokenForm
        refused={refused}
        onToken={(t) => {
          saveToken(t);
          setRefused(false);
          setToken(t);
        }}
      />
    );
  }
  return (
    <QueryClientProvider client={queries}>
      <Dashboard client={client} token={token} />
    </QueryClientProvider>
  );
}

function TokenForm({ refused, onToken }: { refused: boolean; onToken: (t: string) => void }) {
  const [value, setValue] = useState('');
  return (
    <form
      className="sim-token"
      onSubmit={(e) => {
        e.preventDefault();
        if (value.trim()) onToken(value.trim());
      }}
    >
      <h1>Simulator</h1>
      <p>
        The control token is the simulator's <code>FINUANCE_SIM_CONTROL_TOKEN</code>. <code>make sim-up</code> and{' '}
        <code>make observe</code> print an address that carries it.
      </p>
      {refused && (
        <p className="sim-error" role="alert">
          The simulator refused that token. Check FINUANCE_SIM_CONTROL_TOKEN and try again.
        </p>
      )}
      <label>
        Control token
        <TextInput
          type="password"
          autoComplete="off"
          value={value}
          onChange={(e) => {
            setValue(e.target.value);
          }}
        />
      </label>
      <Button type="submit" variant="primary">
        Open
      </Button>
    </form>
  );
}
