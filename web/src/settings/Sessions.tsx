import { useRevokeSession, useSessions, type Session } from '../api/hooks/sessions';
import { fmtDateTime } from '../lib/dates';
import { errorMessage } from '../lib/errors';
import { Button } from '../ui/Button';
import { toast } from '../ui/toastStore';

export function SessionsSettings() {
  const sessions = useSessions();
  const revoke = useRevokeSession();
  return (
    <div className="set-card">
      <div className="lbl">Signed-in browsers</div>
      <p className="hint">Sign out a browser you no longer use. Its next request goes back to the sign-in page.</p>
      {sessions.isError && <p className="set-err">{errorMessage(sessions.error)}</p>}
      <ul className="set-list">
        {sessions.data?.map((s) => (
          <li key={s.id}>
            <div className="set-li-main">
              <b>{browserName(s.userAgent)}</b>
              {s.current && <span className="tag src">this browser</span>}
              <span className="hint">
                Last active {fmtDateTime(s.lastSeenAt)}
                {s.ip ? ` · ${s.ip}` : ''} · signed in {fmtDateTime(s.createdAt)}
              </span>
            </div>
            {!s.current && (
              <Button
                size="sm"
                disabled={revoke.isPending}
                onClick={() => {
                  revoke.mutate(s.id, {
                    onSuccess: () => {
                      toast(`Signed out ${browserName(s.userAgent)}`);
                    },
                    onError: (e) => {
                      toast(errorMessage(e), { kind: 'error' });
                    },
                  });
                }}
              >
                Sign out
              </Button>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}

/** A short name for a user agent: "Firefox on macOS". Unknown agents show as given. */
export function browserName(ua: Session['userAgent']): string {
  if (!ua) return 'Unknown browser';
  const browser = /Edg\//.test(ua)
    ? 'Edge'
    : /OPR\//.test(ua)
      ? 'Opera'
      : /Firefox\//.test(ua)
        ? 'Firefox'
        : /Chrome\//.test(ua)
          ? 'Chrome'
          : /Safari\//.test(ua)
            ? 'Safari'
            : null;
  const os = /iPhone|iPad/.test(ua)
    ? 'iOS'
    : /Android/.test(ua)
      ? 'Android'
      : /Mac OS X|Macintosh/.test(ua)
        ? 'macOS'
        : /Windows/.test(ua)
          ? 'Windows'
          : /Linux/.test(ua)
            ? 'Linux'
            : null;
  if (!browser) return ua.length > 60 ? `${ua.slice(0, 57)}…` : ua;
  return os ? `${browser} on ${os}` : browser;
}
