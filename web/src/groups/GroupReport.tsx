// The group window (PLAN 15.9): a group's totals and date span, money out per day, a categories
// donut, the top payees and the budget's progress, with "Show in table" and a CSV export of the
// group. Like the group stats, it counts every account and includes transfers.

import { useState } from 'react';

import { exportCsv } from '../api/hooks/transactions';
import { useGroupReport, type Group, type GroupReport } from '../api/hooks/groups';
import { slotColor } from '../charts/lib/colors';
import { arc } from '../charts/lib/geom';
import { money, whole } from '../charts/lib/money';
import { Svg } from '../charts/lib/Svg';
import { TipContent } from '../charts/lib/Tip';
import { UNCATEGORIZED } from '../charts/CategoriesChart';
import { fmtDay, fmtDayMedium } from '../lib/dates';
import { errorMessage } from '../lib/errors';
import { fmtCompact, fmtInt, fmtShare, plural } from '../lib/format';
import { saveBlob } from '../table/Toolbar';
import { Button } from '../ui/Button';
import { Modal } from '../ui/Modal';
import { toast } from '../ui/toastStore';
import { useTip } from '../ui/Tooltip';
import { groupColor } from './colors';

/** The columns of the group's CSV export. */
export const GROUP_EXPORT_COLUMNS = [
  'bookedOn',
  'payee',
  'description',
  'amount',
  'currency',
  'category',
  'account',
  'note',
] as const;

/** A budget's progress: spent / budget, "N left" or "over N" (the overview's budget colours). */
export function BudgetBar({ spent, budget, cur }: { spent: number; budget: number; cur: string }) {
  const over = spent > budget;
  const share = budget ? spent / budget : 1;
  const color = over ? 'var(--st-crit)' : share > 0.85 ? 'var(--st-warn)' : 'var(--st-good)';
  return (
    <span className="gbudget" title={`${whole(spent, cur)} of ${whole(budget, cur)} (${fmtShare(share)})`}>
      <span className="bbar">
        <span className="bfill" style={{ width: `${(Math.min(1, share) * 100).toFixed(1)}%`, background: color }} />
      </span>
      <span className="gbv" style={over ? { color } : undefined}>
        {over ? `over ${whole(spent - budget, cur)}` : `${whole(budget - spent, cur)} left`}
      </span>
    </span>
  );
}

function Kpi({ label, value, sub }: { label: string; value: string; sub?: string }) {
  return (
    <div className="kpi" role="group" aria-label={label}>
      <span className="l">{label}</span>
      <span className="n">{value}</span>
      {sub && <span className="d">{sub}</span>}
    </div>
  );
}

const DAY_W = 640;
const DAY_H = 120;

function DayBar({
  x,
  y,
  w,
  h,
  date,
  v,
  cur,
}: {
  x: number;
  y: number;
  w: number;
  h: number;
  date: string;
  v: number;
  cur: string;
}) {
  const tip = useTip(<TipContent title={fmtDayMedium(date)} rows={[['var(--exp)', 'Spent', money(v, cur)]]} />);
  return <rect x={x} y={y} width={w} height={h} rx={1.5} style={{ fill: 'var(--exp)' }} data-date={date} {...tip} />;
}

/** Money out on every day from the first to the last transaction (days without any stay empty). */
function Daily({ r }: { r: GroupReport }) {
  const first = r.group.firstOn;
  if (!first || !r.daily.length) return <p className="hint">No money out in this group.</p>;
  const start = Date.parse(`${first}T00:00:00Z`);
  const max = Math.max(...r.daily.map((d) => d.spentMinor));
  const step = DAY_W / r.days;
  const w = Math.max(1, Math.min(18, step - (step > 4 ? 2 : 0)));
  const chartH = DAY_H - 16;
  return (
    <Svg w={DAY_W} h={DAY_H} label="Money out per day">
      {r.daily.map((d) => {
        const i = Math.round((Date.parse(`${d.date}T00:00:00Z`) - start) / 86_400_000);
        const h = Math.max(1, (d.spentMinor / max) * (chartH - 4));
        return (
          <DayBar
            key={d.date}
            x={i * step + (step - w) / 2}
            y={chartH - h}
            w={w}
            h={h}
            date={d.date}
            v={d.spentMinor}
            cur={r.currency}
          />
        );
      })}
      <text x={0} y={DAY_H - 2} className="ax">
        {fmtDay(first)}
      </text>
      <text x={DAY_W} y={DAY_H - 2} textAnchor="end" className="ax">
        {fmtDay(r.group.lastOn)}
      </text>
    </Svg>
  );
}

type Slice = { name: string; v: number; share: number; color: string };

function SliceRow({ s, cur }: { s: Slice; cur: string }) {
  return (
    <div className="lg">
      <i className="sw" style={{ background: s.color }} />
      <span className="nm">{s.name}</span>
      <span className="vl">{whole(s.v, cur)}</span>
      <span className="pc">{fmtShare(s.share)}</span>
    </div>
  );
}

function SlicePath({ s, d, cur }: { s: Slice; d: string; cur: string }) {
  const tip = useTip(
    <TipContent
      title={s.name}
      rows={[
        [s.color, 'Spent', money(s.v, cur)],
        ['', 'Share', fmtShare(s.share, 1)],
      ]}
    />,
  );
  return <path d={d} style={{ fill: s.color, stroke: 'var(--panel)', strokeWidth: 2 }} {...tip} />;
}

/** Money out per category: the categories with a colour slot, the rest as "Other". */
function Categories({ r }: { r: GroupReport }) {
  const total = r.categories.reduce((a, c) => a + c.spentMinor, 0);
  if (!total) return <p className="hint">No money out in this group.</p>;
  const slices: Slice[] = [];
  let other = 0;
  let otherN = 0;
  for (const c of r.categories) {
    if (c.colorSlot) {
      slices.push({
        name: c.category?.name ?? UNCATEGORIZED,
        v: c.spentMinor,
        share: c.spentMinor / total,
        color: slotColor(c.colorSlot),
      });
    } else {
      other += c.spentMinor;
      otherN++;
    }
  }
  if (other) slices.push({ name: `Other (${String(otherN)})`, v: other, share: other / total, color: slotColor(null) });
  const R = 60;
  const rr = R * 0.64;
  let a0 = -Math.PI / 2;
  return (
    <div className="donut">
      <Svg w={2 * R + 2} h={2 * R + 2} label="Money out by category">
        {slices.length === 1 ? (
          <circle
            cx={R + 1}
            cy={R + 1}
            r={(R + rr) / 2}
            style={{ fill: 'none', stroke: slices[0]?.color, strokeWidth: R - rr }}
          />
        ) : (
          slices.map((s) => {
            const a1 = a0 + s.share * Math.PI * 2;
            const d = arc(R + 1, R + 1, R, rr, a0, a1);
            a0 = a1;
            return <SlicePath key={s.name} s={s} d={d} cur={r.currency} />;
          })
        )}
        <text x={R + 1} y={R} textAnchor="middle" style={{ font: '600 15px var(--sans)', fill: 'var(--text)' }}>
          {fmtCompact({ amountMinor: total, currency: r.currency })}
        </text>
        <text x={R + 1} y={R + 14} textAnchor="middle" className="ax">
          spent
        </text>
      </Svg>
      <div className="leg">
        {slices.map((s) => (
          <SliceRow key={s.name} s={s} cur={r.currency} />
        ))}
      </div>
    </div>
  );
}

function Payees({ r }: { r: GroupReport }) {
  if (!r.payees.length) return <p className="hint">No payees with money out.</p>;
  const max = r.payees[0]?.spentMinor ?? 1;
  return (
    <div className="gpayees" role="list" aria-label="Top payees">
      {r.payees.map((p) => (
        <div key={p.payee} className="hb" role="listitem">
          <span className="hb-l">
            <span>{p.payee || '(no payee)'}</span>
          </span>
          <span className="hb-t">
            <span
              className="hb-f"
              style={{ width: `${((p.spentMinor / max) * 100).toFixed(2)}%`, background: 'var(--exp)' }}
            />
          </span>
          <span className="hb-n" title={plural(p.count, 'payment')}>
            {fmtInt(p.count)}×
          </span>
          <span className="hb-v">{whole(p.spentMinor, r.currency)}</span>
        </div>
      ))}
      {r.payeeCount > r.payees.length && (
        <p className="hint">and {plural(r.payeeCount - r.payees.length, 'more payee')}</p>
      )}
    </div>
  );
}

type Props = { group: Group; onClose: () => void; onShow: () => void };

export function GroupReportWindow({ group, onClose, onShow }: Props) {
  const q = useGroupReport(group.id);
  const [exporting, setExporting] = useState(false);
  const r = q.data;
  const g = r?.group ?? group;
  const cur = g.spent.currency;
  const doExport = async () => {
    setExporting(true);
    try {
      const { blob, filename } = await exportCsv({
        context: { groupId: g.id, includeTransfers: true },
        sort: { column: 'bookedOn', dir: 'asc' },
        columns: [...GROUP_EXPORT_COLUMNS],
        delimiter: ';',
        view:
          g.name
            .normalize('NFD')
            .replace(/[̀-ͯ]/g, '')
            .replace(/[^A-Za-z0-9_-]+/g, '-')
            .replace(/^-+|-+$/g, '')
            .toLowerCase()
            .slice(0, 60) || 'group',
      });
      saveBlob(blob, filename);
      toast(`Exported ${plural(g.count, 'row')}`);
    } catch (e) {
      toast(errorMessage(e), { kind: 'error' });
    } finally {
      setExporting(false);
    }
  };
  const span =
    g.firstOn && g.lastOn
      ? g.firstOn === g.lastOn
        ? fmtDay(g.firstOn)
        : `${fmtDay(g.firstOn)} – ${fmtDay(g.lastOn)}`
      : 'no transactions';
  return (
    <Modal
      open
      onClose={onClose}
      title={g.name}
      sub={
        <>
          <i className="gdot-i" style={{ background: groupColor(g.color) }} /> {plural(g.count, 'transaction')}
        </>
      }
      size="md"
    >
      <div className="grep">
        {q.isError && <p className="set-err">{errorMessage(q.error)}</p>}
        <div className="kpis">
          <Kpi label="Spent" value={money(g.spent.amountMinor, cur)} />
          <Kpi label="Income" value={money(g.income.amountMinor, cur)} />
          <Kpi label="Span" value={r ? plural(r.days, 'day') : '…'} sub={span} />
          <Kpi label="Per day" value={r ? money(r.avgPerDayMinor, cur) : '…'} sub="money out" />
        </div>
        {g.budgetMinor !== undefined && (
          <section className="gsec" aria-label="Budget">
            <h3>
              Budget <span className="muted">{whole(g.budgetMinor, cur)}</span>
            </h3>
            <BudgetBar spent={g.spent.amountMinor} budget={g.budgetMinor} cur={cur} />
          </section>
        )}
        {r && (
          <>
            <section className="gsec" aria-label="Money out per day">
              <h3>Money out per day</h3>
              <Daily r={r} />
            </section>
            <div className="gcols">
              <section className="gsec" aria-label="Categories">
                <h3>Categories</h3>
                <Categories r={r} />
              </section>
              <section className="gsec" aria-label="Top payees">
                <h3>Top payees</h3>
                <Payees r={r} />
              </section>
            </div>
          </>
        )}
        <p className="hint">All accounts, transfers included.</p>
        <div className="set-actions">
          <Button onClick={onShow}>Show in table</Button>
          <Button icon="download" disabled={exporting || !g.count} onClick={() => void doExport()}>
            Export CSV
          </Button>
        </div>
      </div>
    </Modal>
  );
}
