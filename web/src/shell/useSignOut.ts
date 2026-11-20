import { useNavigate } from 'react-router';

import { useLogout } from '../api/hooks/me';
import { flushPrefs, resetSession } from '../stores/persist';

/** Saves pending preferences, leaves the app, forgets what it held in memory and ends the session. */
export function useSignOut(): () => Promise<void> {
  const navigate = useNavigate();
  const logout = useLogout();
  return async () => {
    await flushPrefs().catch(() => undefined);
    await navigate('/login', { replace: true });
    resetSession(); // nothing of this account stays in memory for whoever signs in next (WEB-1)
    logout.mutate();
  };
}
