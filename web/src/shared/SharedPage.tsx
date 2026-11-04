// The Shared tab (Phase 19, ADR 0028): /s is the timeline of personal and shared groups with
// invitations, per-year sums, a comparison of the groups and "next time" hints; /s/:groupId is one
// shared group. Its own chunk, loaded when the tab first opens.

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
import { ShareDialog } from './dialogs';
import { GroupPage } from './GroupPage';
import { hintText, TIMELINE_KIND_LABEL } from './text';

export function SharedPage() {
  const { groupId } = useParams();
  const meta = useMeta();
  if (meta.data && !meta.data.features.sharedGroups) return <Navigate to="/" replace />;
  return groupId ? <GroupPage key={groupId} id={groupId} /> : <Timeline />;
}

type KindFilter = 'all' | TimelineGroup['kind'];

function span(g: TimelineGroup): string {
  if (!g.firstOn || !g.lastOn) return 'no expenses yet';
  return g.firstOn === g.lastOn ? fmtDay(g.firstOn) : `${fmtDay(g.firstOn)} – ${fmtDay(g.lastOn)}`;
}

function Timeline() {
  const tl = useTimeline();
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
    if (g.source === 'shared') void navigate(`/s/${g.id}`);
  };
  return (
    <div className="sh-page">
      <header className="sh-head">
        <div className="sh-title">
          <h1>Shared</h1>
          <span className="muted">Trips and groups, yours and shared with friends</span>
          <span className="sh-tools">
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
                    void navigate(`/s/${g.id}`);
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
