// What the router shows when a screen fails to draw (PLAN 24.8). The error is reported to the
// server by kind, class name and route pattern only (lib/clientEvents); the person gets a way on.

import { useEffect } from 'react';
import { useRouteError } from 'react-router';

import { reportClientEvent } from '../lib/clientEvents';
import { Button } from '../ui/Button';
import { useAppearance } from './effects';

export function ScreenError() {
  useAppearance();
  const error = useRouteError();
  useEffect(() => {
    reportClientEvent('render', error);
  }, [error]);
  return (
    <main className="auth">
      <div className="dz lockcard" role="alert">
        <h2>This screen couldn't be shown</h2>
        <p>
          Something went wrong while drawing it. Your data is not affected. The error has been reported to this
          instance's log.
        </p>
        <Button
          variant="primary"
          onClick={() => {
            window.location.assign('/');
          }}
        >
          Back to the start
        </Button>
      </div>
    </main>
  );
}
