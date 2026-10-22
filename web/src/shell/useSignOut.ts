import { useNavigate } from 'react-router';

import { useLogout } from '../api/hooks/me';
import { flushPrefs } from '../stores/persist';

/** Saves pending preferences, leaves the app and ends the session. */
export function useSignOut(): () => Promise<void> {
  const navigate = useNavigate();
  const logout = useLogout();
  return async () => {
    await flushPrefs().catch(() => undefined);
    await navigate('/login', { replace: true });
    logout.mutate();
  };
}
