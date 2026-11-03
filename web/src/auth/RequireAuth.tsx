// The auth gate (PLAN §7.3): loads GET /me, then GET /preferences, hydrates the stores and starts
// the preferences sync before the app renders, so defaults never overwrite saved preferences
// (legacy S.booted). A 401 is sent to /login?next=… by the API client.

import { useEffect, type ReactNode } from 'react';

import { ApiError } from '../api/client';
import { useMe, useMeta, type User } from '../api/hooks/me';
import { usePrefs } from '../api/hooks/prefs';
import { errorMessage } from '../lib/errors';
import { startPrefsSync, usePrefsReady } from '../stores/persist';
import { useAppearance } from '../shell/effects';
import { Button } from '../ui/Button';
import { toast } from '../ui/toastStore';
import { ChoosePassword } from './ChoosePassword';

export function RequireAuth({ children }: { children: (user: User) => ReactNode }) {
  const me = useMe();
  const meta = useMeta();
  const prefs = usePrefs(me.isSuccess);
  const readyFor = usePrefsReady((s) => s.userId);
  const userId = me.data?.id;
  const doc = prefs.data;
  // the instance's view defaults (Phase 19) sit under the user's saved preferences; a failed
  // /meta just means no defaults
  const metaSettled = meta.isSuccess || meta.isError;
  const defaults = meta.data?.defaults;

  useEffect(() => {
    if (!userId || !doc || !metaSettled) return;
    return startPrefsSync(
      userId,
      doc,
      () => {
        toast("Your settings couldn't be saved. They'll be saved with your next change.", { kind: 'error' });
      },
      defaults,
    );
  }, [userId, doc, metaSettled, defaults]);

  const failed = me.error ?? prefs.error;
  if (failed && !(failed instanceof ApiError && failed.status === 401)) return <BootError error={failed} />;
  if (me.data?.mustChangePassword) return <ChoosePassword />;
  if (!me.data || readyFor !== me.data.id) return <div className="app booting" />;
  return children(me.data);
}

function BootError({ error }: { error: Error }) {
  useAppearance();
  return (
    <main className="auth">
      <div className="dz lockcard" role="alert">
        <h2>Finuance couldn't start</h2>
        <p>{errorMessage(error)}</p>
        <Button
          variant="primary"
          onClick={() => {
            window.location.reload();
          }}
        >
          Try again
        </Button>
      </div>
    </main>
  );
}
