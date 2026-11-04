// The bell in the title bar (Phase 19, 19.8): what other people did that concerns you, newest
// first, with the unread count. Checked every minute and when the window gets focus; opening the
// list marks everything read. In the app only, never e-mail. The words come from the kind and
// the data (display names, never e-mail addresses).

import { useNavigate } from 'react-router';

import { useMarkNotificationsRead, useNotifications, type Notification } from '../api/hooks/friends';
import { fmtAgo } from '../lib/dates';
import { IconButton } from '../ui/IconButton';
import { Popover, usePopover } from '../ui/Popover';

/** What a notification says. Every kind of the API has an entry (the compiler checks). */
const SAY: Record<Notification['kind'], (d: Record<string, string>) => string> = {
  'friend.added': (d) => `${d['name'] || 'Someone'} entered your code: you are friends now`,
  'friend.removed': (d) => `${d['name'] || 'A friend'} removed you as a friend`,
};

/** Where a notification leads. */
const LINK: Record<Notification['kind'], string> = {
  'friend.added': '/settings/friends',
  'friend.removed': '/settings/friends',
};

export function describeNotification(n: Notification): string {
  return SAY[n.kind](n.data);
}

export function NotificationsButton() {
  const list = useNotifications();
  const read = useMarkNotificationsRead();
  const pop = usePopover();
  const navigate = useNavigate();
  const unread = list.data?.unread ?? 0;
  const items = list.data?.items ?? [];
  const label = unread ? `Notifications, ${String(unread)} unread` : 'Notifications';
  return (
    <>
      <span className="bell">
        <IconButton
          icon="bell"
          title={label}
          aria-label={label}
          aria-expanded={pop.open}
          onClick={(e) => {
            if (!pop.open && unread) read.mutate();
            pop.toggle(e);
          }}
        />
        {unread > 0 && (
          <span className="bell-n" aria-hidden="true">
            {unread > 9 ? '9+' : String(unread)}
          </span>
        )}
      </span>
      <Popover open={pop.open} anchor={pop.anchor} onClose={pop.close} label="Notifications" width={320}>
        <div className="nt">
          <div className="nt-h">Notifications</div>
          {items.length === 0 ? (
            <p className="hint nt-empty">
              Nothing yet. Friends and shared groups tell you here when they do something.
            </p>
          ) : (
            <ul className="nt-list">
              {items.map((n) => (
                <li key={n.id} className={n.readAt ? undefined : 'new'}>
                  <button
                    type="button"
                    onClick={() => {
                      pop.close();
                      void navigate(LINK[n.kind]);
                    }}
                  >
                    <span>{describeNotification(n)}</span>
                    <span className="hint">{fmtAgo(n.createdAt)}</span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      </Popover>
    </>
  );
}
