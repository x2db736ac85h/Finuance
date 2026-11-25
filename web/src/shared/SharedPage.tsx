// The Shared page (Phase 19, ADR 0028): /shared is the timeline of personal and shared groups with
// invitations, per-year sums, a comparison of the groups and "next time" hints, and since Phase 26
// (U8) the friends they can be shared with; /shared/:groupId is one shared group. Its own chunk,
// loaded when the page first opens.

import { useState } from 'react';
import { Navigate, useNavigate, useParams } from 'react-router';

import { useMeta } from '../api/hooks/me';
import { useSharedGroups, useTimeline, type TimelineGroup } from '../api/hooks/shared';
import { money } from '../charts/lib/money';
import { groupColor } from '../groups/colors';
import { fmtDay } from '../lib/dates';
import { errorMessage } from '../lib/errors';
import { fmtInt, fmtShare, plural } from '../lib/format';
import { Button } from '../ui/Button';
import { Select } from '../ui/inputs';
import { useScrollToHash } from '../lib/useScrollToHash';
import { ShareDialog } from './dialogs';
import { FriendsPanel } from './Friends';
import { GroupPage } from './GroupPage';
import { hintText, TIMELINE_KIND_LABEL } from './text';
import { sharedPath } from '../lib/paths';

export function SharedPage() {
  const { groupId } = useParams();
  const meta = useMeta();
  const groupsOn = meta.data?.features.sharedGroups !== false;
  // friends only where the instance has them: switched on, and not a demo (ADR 0027)
  const friendsOn = meta.data?.features.friends !== false && !meta.data?.demoMode;
  if (meta.data && !groupsOn && !friendsOn) return <Navigate to="/" replace />;
  if (meta.data && !groupsOn) return groupId ? <Navigate to="/shared" replace /> : <FriendsOnly />;
  return groupId ? <GroupPage key={groupId} id={groupId} /> : <Timeline friends={friendsOn} />;
}

/** An instance with friends but without shared groups: the page is the friends alone. */
function FriendsOnly() {
  useScrollToHash();
  return (
    <div className="sh-page">
      <header className="sh-head">
        <div className="sh-title">
          <h1>Shared</h1>
          <span className="muted">Your friends on this instance</span>
        </div>
      </header>
      <FriendsPanel />
    </div>
  );
}

type KindFilter = 'all' | TimelineGroup['kind'];

function span(g: TimelineGroup): string {
  if (!g.firstOn || !g.lastOn) return 'no expenses yet';
  return g.firstOn === g.lastOn ? fmtDay(g.firstOn) : `${fmtDay(g.firstOn)} – ${fmtDay(g.lastOn)}`;
}

function Timeline({ friends }: { friends: boolean }) {
  const tl = useTimeline();
  useScrollToHash(tl.isSuccess);
  const shared = useSharedGroups();
  const navigate = useNavigate();
  const [kind, setKind] = useState<KindFilter>('all');
  const [creating, setCreating] = useState(false);
  const invites = (shared.data ?? []).filter((g) => g.me.state === 'invited');
  const data = tl.data;
  const groups = (data?.groups ?? []).filter((g) => kind === 'all' || g.kind === kind);
  const kinds = [...new Set((data?.groups ?? []).map((g) => g.kind))];
  const byId = new Map((data?.groups ?? []).map((g) => [g.id, g]));
  const open = (g: TimelineGroup) => {
    if (g.source === 'shared') void navigate(sharedPath(g.id));
  };
  return (
    <div className="sh-page">
      <header className="sh-head">
        <div className="sh-title">
          <h1>Shared</h1>
          <span className="muted">Trips and groups, yours and shared with friends</span>
          <span className="sh-tools">
            {friends && (
              <Button
                size="sm"
                icon="users"
                onClick={() => {
                  void navigate({ hash: '#friends' });
                }}
              >
                Friends
              </Button>
            )}
            <Button
              size="sm"
              icon="plus"
              onClick={() => {
                setCreating(true);
              }}
            >
              New shared group
            </Button>
          </span>
        </div>
      </header>
      {tl.isError && <p className="set-err">{errorMessage(tl.error)}</p>}
      {invites.length > 0 && (
        <section className="sh-invite" aria-label="Invitations">
          <h2>Invitations</h2>
          <ul>
            {invites.map((g) => (
              <li key={g.id}>
                <b>{g.name}</b>
                <Button
                  size="sm"
                  onClick={() => {
                    void navigate(sharedPath(g.id));
                  }}
                >
                  Open
                </Button>
              </li>
            ))}
          </ul>
        </section>
      )}
      {data && data.hints.length > 0 && (
        <section className="sh-hints" aria-label="Next time">
          <h2>Next time</h2>
          <ul>
            {data.hints.map((h) => (
              <li key={`${h.groupId}-${h.kind}`}>{hintText(h, byId.get(h.groupId))}</li>
            ))}
          </ul>
        </section>
      )}
      {data && data.years.length > 0 && (
        <section className="sh-years" aria-label="Per year">
          <h2>Per year</h2>
          <table className="adm-table">
            <thead>
              <tr>
                <th>Year</th>
                <th className="num">Groups</th>
                <th className="num">Days</th>
                <th className="num">Spent</th>
              </tr>
            </thead>
            <tbody>
              {data.years.map((y) => (
                <tr key={y.year}>
                  <td>{y.year}</td>
                  <td className="num">{fmtInt(y.groups)}</td>
                  <td className="num">{fmtInt(y.days)}</td>
                  <td className="num">{money(y.spentMinor, y.currency)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>
      )}
      <section className="sh-timeline" aria-label="Timeline">
        <div className="sh-bal-h">
          <h2>Timeline</h2>
          {kinds.length > 1 && (
            <Select
              aria-label="Show"
              value={kind}
              onChange={setKind}
              options={[
                { value: 'all', label: 'Every kind' },
                ...kinds.map((k) => ({ value: k, label: TIMELINE_KIND_LABEL[k] })),
              ]}
            />
          )}
        </div>
        {data && !data.groups.length && (
          <p className="hint">
            No groups yet. Make a group in the table (Groups → New group), put a trip's transactions in it, then share
            it here or from the group menu — or start an empty shared group and add expenses as you go.
          </p>
        )}
        {groups.length > 0 && (
          <table className="adm-table sh-table">
            <thead>
              <tr>
                <th>Group</th>
                <th>Dates</th>
                <th className="num">Days</th>
                <th className="num">Spent</th>
                <th className="num">Per day</th>
                <th>Largest category</th>
              </tr>
            </thead>
            <tbody>
              {groups.map((g) => (
                <tr key={g.id} className={g.source === 'shared' ? 'sh-row' : undefined}>
                  <td>
                    <i className="gdot-i" style={{ background: groupColor(g.color) }} />{' '}
                    {g.source === 'shared' ? (
                      <button
                        type="button"
                        className="adm-link"
                        onClick={() => {
                          open(g);
                        }}
                      >
                        {g.name}
                      </button>
                    ) : (
                      g.name
                    )}{' '}
                    <span className="sh-badge">{g.source === 'shared' ? 'shared' : TIMELINE_KIND_LABEL[g.kind]}</span>
                  </td>
                  <td>{span(g)}</td>
                  <td className="num">{fmtInt(g.days)}</td>
                  <td className="num">{money(g.spentMinor, g.currency)}</td>
                  <td className="num">{money(g.perDayMinor, g.currency)}</td>
                  <td>
                    {g.topCategory
                      ? `${g.topCategory.name || '(uncategorised)'} · ${fmtShare(g.topCategoryShare ?? 0)}`
                      : ''}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        {data && (
          <p className="hint">
            {plural(data.groups.length, 'group')}; a shared group's totals are everyone's expenses in it.
          </p>
        )}
      </section>
      {friends && <FriendsPanel />}
      {creating && (
        <ShareDialog
          onClose={() => {
            setCreating(false);
          }}
        />
      )}
    </div>
  );
}
