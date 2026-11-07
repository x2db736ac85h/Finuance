// The bell in the title bar (Phase 19, 19.8): what other people did that concerns you, and the
// assistant's digests (Phase 20), newest first, with the unread count. Checked every minute and
// when the window gets focus; opening the list marks everything read. In the app only, never
// e-mail. The words come from the kind and the data (display names, never e-mail addresses).

import { useNavigate } from 'react-router';

import { useMarkNotificationsRead, useNotifications, type Notification } from '../api/hooks/friends';
import { useChat } from '../assistant/chatStore';
import { fmtAgo } from '../lib/dates';
import { useUi } from '../stores/uiStore';
import { IconButton } from '../ui/IconButton';
import { Popover, usePopover } from '../ui/Popover';
import { toast } from '../ui/toastStore';

/** A digest's notification: which one, and how many proposals wait in it (ADR 0033). */
function sayDigest(d: Record<string, string>): string {
  const which = d['schedule'] === 'weekly' ? 'weekly' : 'daily';
  if (d['failed']) return `Your ${which} digest couldn't be written. Open the assistant to see why.`;
  const n = Number(d['proposals'] || 0);
  const waiting = n === 1 ? ' · 1 proposal waiting' : n > 1 ? ` · ${String(n)} proposals waiting` : '';
  return `Your ${which} digest is ready${waiting}`;
}

/** What a notification says. Every kind of the API has an entry (the compiler checks). */
const SAY: Record<Notification['kind'], (d: Record<string, string>) => string> = {
  'friend.added': (d) => `${d['name'] || 'Someone'} entered your code: you are friends now`,
  'friend.removed': (d) => `${d['name'] || 'A friend'} removed you as a friend`,
  'shared.invited': (d) => `${d['name'] || 'A friend'} invited you to ${d['group'] || 'a shared group'}`,
  'shared.joined': (d) => `${d['name'] || 'A friend'} joined ${d['group'] || 'your shared group'}`,
  'shared.left': (d) => `${d['name'] || 'A member'} left ${d['group'] || 'your shared group'}`,
  'shared.expense_added': (d) => `${d['name'] || 'A member'} added an expense to ${d['group'] || 'a shared group'}`,
  'shared.settled': (d) => `${d['name'] || 'A member'} recorded a payment in ${d['group'] || 'a shared group'}`,
  'assistant.digest': sayDigest,
};

/** Where a notification leads. */
const LINK: Record<Notification['kind'], string> = {
  'friend.added': '/settings/friends',
  'friend.removed': '/settings/friends',
  'shared.invited': '/s',
  'shared.joined': '/s',
  'shared.left': '/s',
  'shared.expense_added': '/s',
  'shared.settled': '/s',
  'assistant.digest': '',
};

/** A digest opens its chat in the assistant panel, wherever the user is. */
function openDigest(n: Notification) {
  useUi.getState().setAiOpen(true);
  const id = n.data['chatId'];
  if (id)
    void useChat
      .getState()
      .open(id)
      .catch(() => {
        toast('This digest is gone (expired or deleted)', { kind: 'error' });
      });
}

/** Where a notification leads: a shared-group notification opens its group. */
function linkOf(n: Notification): string {
  const g = n.data['groupId'];
  return n.kind.startsWith('shared.') && g && n.kind !== 'shared.left' ? `/s/${g}` : LINK[n.kind];
}

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
              Nothing yet. Friends, shared groups and the assistant's digests tell you here.
            </p>
          ) : (
            <ul className="nt-list">
              {items.map((n) => (
                <li key={n.id} className={n.readAt ? undefined : 'new'}>
                  <button
                    type="button"
                    onClick={() => {
                      pop.close();
                      if (n.kind === 'assistant.digest') openDigest(n);
                      else void navigate(linkOf(n));
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
