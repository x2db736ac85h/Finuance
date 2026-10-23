// The investment cards (PLAN §4.9, legacy o_inv, o_yoc, o_fire): Investment (allocation, PPK and
// positions), Yield on cost and the FI/RE number. The server computes them from the stored
// document; the values are typed in only in the larger view (PPK, positions, the safe withdrawal
// rate and the expected real return), and every change saves itself (useEditInvestments).

import type { Schemas } from '../api/client';
import { useEditInvestments, useInvestments, type Investments } from '../api/hooks/overview';
import { PRIMARY } from '../charts/lib/colors';
import { money, whole } from '../charts/lib/money';
import { TipContent } from '../charts/lib/Tip';
import { Empty, type ChartDef, type ChartEnv, type ChartParts } from '../charts/types';
import { parseMoneyInput } from '../lib/format';
import { toast } from '../ui/toastStore';
import { Icon } from '../ui/Icon';
import { useTip } from '../ui/Tooltip';
import { inputText } from './Budgets';
import { pctS, useOverviewParts, type OverviewData } from './data';

type Row = Schemas['InvestmentRow'];
type Position = Investments['positions'][number];

/** Investment amounts are entered in PLN (legacy). */
const PLN = 'PLN';

const gainCls = (g: number) => (g < 0 ? 'neg' : g > 0 ? 'pos' : undefined);

/** A number field of the document; an unreadable value is dropped (the old one comes back). */
function NumIn({
  value,
  label,
  min,
  onSave,
}: {
  value: number;
  label: string;
  min?: number;
  onSave: (v: number) => void;
}) {
  const text = inputText(value, PLN);
  return (
    <input
      key={text}
      className="bud-in inv-in"
      defaultValue={text}
      inputMode="decimal"
      aria-label={label}
      onBlur={(e) => {
        const el = e.currentTarget;
        const v = el.value.trim() === '' ? 0 : parseMoneyInput(el.value, PLN);
        if (v === null || v === value) {
          el.value = text;
          return;
        }
        onSave(min === undefined ? v : Math.max(min, v));
      }}
      onKeyDown={(e) => {
        if (e.key === 'Enter') e.currentTarget.blur();
      }}
    />
  );
}

function TextIn({
  value,
  label,
  max,
  required,
  onSave,
}: {
  value: string;
  label: string;
  max: number;
  required?: boolean;
  onSave: (v: string) => void;
}) {
  return (
    <input
      key={value}
      className="inv-in txt"
      defaultValue={value}
      maxLength={max}
      spellCheck={false}
      aria-label={label}
      onBlur={(e) => {
        const el = e.currentTarget;
        const v = el.value.trim();
        if (v === value || (required && !v)) {
          el.value = value;
          return;
        }
        onSave(v);
      }}
      onKeyDown={(e) => {
        if (e.key === 'Enter') e.currentTarget.blur();
      }}
    />
  );
}

function Alloc({ s }: { s: Schemas['InvestmentSummary'] }) {
  if (s.valueMinor <= 0) return null;
  return (
    <div className="alloc">
      {s.rows
        .filter((r) => r.valueMinor > 0)
        .map((r, j) => (
          <AllocPart key={r.id ?? 'ppk'} r={r} j={j} />
        ))}
    </div>
  );
}

function AllocPart({ r, j }: { r: Row; j: number }) {
  const tip = useTip(
    <TipContent
      title={r.name}
      rows={[
        ['', 'Value', money(r.valueMinor, PLN)],
        ['', 'Share', `${(r.allocationShare * 100).toFixed(1)}%`],
      ]}
    />,
  );
  return <i style={{ flex: r.valueMinor, background: `var(--s${(j % 7) + 1})` }} {...tip} />;
}

/** The editors of the larger view (legacy o_inv big). */
function Editor({ s }: { s: Schemas['InvestmentSummary'] }) {
  const doc = useInvestments().data;
  const edit = useEditInvestments();
  if (!doc) return null;
  const ppk = (k: keyof Investments['ppk']) => (v: number) => {
    edit((d) => ({ ...d, ppk: { ...d.ppk, [k]: v } }));
  };
  const pos = (id: string, patch: Partial<Position>) => {
    edit((d) => ({ ...d, positions: d.positions.map((p) => (p.id === id ? { ...p, ...patch } : p)) }));
  };
  const remove = (p: Position) => {
    const at = doc.positions.findIndex((x) => x.id === p.id);
    edit((d) => ({ ...d, positions: d.positions.filter((x) => x.id !== p.id) }));
    toast(`Removed “${p.name}”`, {
      actions: [
        {
          label: 'Undo',
          run: () => {
            edit((d) => ({ ...d, positions: [...d.positions.slice(0, at), p, ...d.positions.slice(at)] }));
          },
        },
      ],
    });
  };
  const P = doc.ppk;
  const paidIn = s.ppkPaidInMinor;
  const gain = s.valueMinor - s.investedMinor;
  return (
    <>
      <div className="inv-sec">
        <div className="lbl">PPK — Pracownicze Plany Kapitałowe</div>
        <div className="inv-grid">
          <label>
            Your contributions
            <NumIn value={P.employeeMinor} label="Your contributions" min={0} onSave={ppk('employeeMinor')} />
          </label>
          <label>
            Employer contributions
            <NumIn value={P.employerMinor} label="Employer contributions" min={0} onSave={ppk('employerMinor')} />
          </label>
          <label>
            State top-ups (dopłaty)
            <NumIn value={P.stateMinor} label="State top-ups" min={0} onSave={ppk('stateMinor')} />
          </label>
          <label>
            Current value
            <NumIn value={P.valueMinor} label="Current value" min={0} onSave={ppk('valueMinor')} />
          </label>
        </div>
        <div className="hint">
          Paid in {money(paidIn, PLN)} · gain {money(s.ppkGainMinor, PLN)} (
          {paidIn ? pctS(s.ppkGainMinor / paidIn) : '–'}) · return on your own money {pctS(s.ppkReturnOnOwn)}
        </div>
      </div>
      <div className="inv-sec">
        <div className="lbl">
          Other investments
          <span className="spacer" />
          <button
            type="button"
            className="btn sm"
            onClick={() => {
              edit((d) => ({
                ...d,
                positions: [
                  ...d.positions,
                  {
                    id: crypto.randomUUID(),
                    name: 'New position',
                    type: '',
                    investedMinor: 0,
                    valueMinor: 0,
                    annualIncomeMinor: 0,
                  },
                ],
              }));
            }}
          >
            <Icon name="plus" />
            Add position
          </button>
        </div>
        {doc.positions.length ? (
          <table className="otab inv-t edit">
            <colgroup>
              <col style={{ width: '24%' }} />
              <col style={{ width: '16%' }} />
              <col />
              <col />
              <col />
              <col style={{ width: '10%' }} />
              <col style={{ width: '2.5rem' }} />
            </colgroup>
            <thead>
              <tr>
                <th>Name</th>
                <th>Type</th>
                <th className="r">Invested (cost)</th>
                <th className="r">Current value</th>
                <th className="r">Annual income</th>
                <th className="r">Gain</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {doc.positions.map((p) => {
                const g = p.valueMinor - p.investedMinor;
                return (
                  <tr key={p.id}>
                    <td>
                      <TextIn
                        value={p.name}
                        label="Name"
                        max={80}
                        required
                        onSave={(name) => {
                          pos(p.id, { name });
                        }}
                      />
                    </td>
                    <td>
                      <TextIn
                        value={p.type}
                        label="Type"
                        max={30}
                        onSave={(type) => {
                          pos(p.id, { type });
                        }}
                      />
                    </td>
                    <td className="r">
                      <NumIn
                        value={p.investedMinor}
                        label={`${p.name} invested`}
                        min={0}
                        onSave={(v) => {
                          pos(p.id, { investedMinor: v });
                        }}
                      />
                    </td>
                    <td className="r">
                      <NumIn
                        value={p.valueMinor}
                        label={`${p.name} value`}
                        min={0}
                        onSave={(v) => {
                          pos(p.id, { valueMinor: v });
                        }}
                      />
                    </td>
                    <td className="r">
                      <NumIn
                        value={p.annualIncomeMinor}
                        label={`${p.name} annual income`}
                        min={0}
                        onSave={(v) => {
                          pos(p.id, { annualIncomeMinor: v });
                        }}
                      />
                    </td>
                    <td className={`r ${gainCls(g) ?? ''}`}>{p.investedMinor ? pctS(g / p.investedMinor) : '–'}</td>
                    <td>
                      <button
                        type="button"
                        className="ib sm"
                        title="Remove position"
                        aria-label={`Remove ${p.name}`}
                        onClick={() => {
                          remove(p);
                        }}
                      >
                        <Icon name="x" />
                      </button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        ) : (
          <div className="hint">
            Add ETFs, stocks, bonds, IKE/IKZE, deposits… Annual income = dividends or interest per year (used for yield
            on cost).
          </div>
        )}
      </div>
      <div className="inv-tot">
        <span>
          Invested <b>{money(s.investedMinor, PLN)}</b>
        </span>
        <span>
          Value <b>{money(s.valueMinor, PLN)}</b>
        </span>
        <span>
          Gain{' '}
          <b className={gainCls(gain)}>
            {money(gain, PLN)} ({pctS(s.gainPercent)})
          </b>
        </span>
        <span>
          Annual income <b>{money(s.incomeMinor, PLN)}</b>
        </span>
      </div>
    </>
  );
}

export function investmentParts(d: OverviewData, _cur: string, env: ChartEnv): ChartParts {
  const s = d.investment;
  const gain = s.valueMinor - s.investedMinor;
  const table = {
    cols: ['Position', 'Invested', 'Value', 'Annual income', 'Gain', 'Gain %'],
    num: [false, true, true, true, true, true],
    rows: s.rows.map((r) => [
      r.name,
      money(r.investedMinor, PLN),
      money(r.valueMinor, PLN),
      money(r.incomeMinor, PLN),
      money(r.valueMinor - r.investedMinor, PLN),
      pctS(r.gainPercent),
    ]),
  };
  const sub = s.valueMinor ? `value ${money(s.valueMinor, PLN)}` : 'enter your values';
  if (env.big)
    return {
      loading: false,
      error: false,
      sub,
      table,
      body: (
        <div className="inv big">
          <Alloc s={s} />
          <Editor s={s} />
        </div>
      ),
    };
  return {
    loading: false,
    error: false,
    sub,
    table,
    body: (
      <div className="inv">
        <Alloc s={s} />
        <table className="otab inv-t">
          <colgroup>
            <col style={{ width: '34%' }} />
            <col />
            <col />
            <col />
          </colgroup>
          <thead>
            <tr>
              <th>Position</th>
              <th className="r">Invested</th>
              <th className="r">Value</th>
              <th className="r">Gain</th>
            </tr>
          </thead>
          <tbody>
            {s.rows.map((r) => {
              const g = r.valueMinor - r.investedMinor;
              return (
                <tr key={r.id ?? 'ppk'}>
                  <td>
                    {r.name}
                    {r.ppk && <PpkSplit />}
                  </td>
                  <td className="r">{whole(r.investedMinor, PLN)}</td>
                  <td className="r">{whole(r.valueMinor, PLN)}</td>
                  <td className={`r ${gainCls(g) ?? ''}`}>{pctS(r.gainPercent)}</td>
                </tr>
              );
            })}
            <tr className="tot">
              <td>Total</td>
              <td className="r">{whole(s.investedMinor, PLN)}</td>
              <td className="r">{whole(s.valueMinor, PLN)}</td>
              <td className={`r ${gainCls(gain) ?? ''}`}>{pctS(s.gainPercent)}</td>
            </tr>
          </tbody>
        </table>
        <div className="hint inv-hint">Values are entered by hand — open larger ↗ to edit ({PLN}).</div>
      </div>
    ),
  };
}

/** The PPK row's contributions (legacy: yours · employer · state). */
function PpkSplit() {
  const doc = useInvestments().data;
  if (!doc) return null;
  const P = doc.ppk;
  return (
    <div className="sub2">
      yours {whole(P.employeeMinor, PLN)} · employer {whole(P.employerMinor, PLN)} · state {whole(P.stateMinor, PLN)}
    </div>
  );
}

export const investmentChart: ChartDef = {
  key: 'o_inv',
  title: 'Investment',
  use: (q, env) => useOverviewParts(q, env, investmentParts),
};

function YieldBar({ k, y, tot, mx }: { k: string; y: number; tot: boolean; mx: number }) {
  const tip = useTip(<TipContent title={k} rows={[['', 'Yield on cost', `${y.toFixed(2)}%`]]} />);
  return (
    <div className={tot ? 'hb on' : 'hb'} {...tip}>
      <span className="hb-l">
        <span>{tot ? <b>Total</b> : k}</span>
      </span>
      <span className="hb-t">
        <span
          className="hb-f"
          style={{ width: `${((y / mx) * 100).toFixed(2)}%`, background: tot ? PRIMARY : 'var(--c-other)' }}
        />
      </span>
      <span className="hb-v">{y.toFixed(2)}%</span>
    </div>
  );
}

export function yieldParts(d: OverviewData): ChartParts {
  const y = d.yieldOnCost;
  const total = y.total ?? 0;
  const detLine =
    y.detectedMinor > 0 ? (
      <div className="hint">
        Dividends &amp; interest found in your transactions (12 months): <b>{money(y.detectedMinor, PLN)}</b>
        {y.usesDetected ? ' — used as income' : ''}
      </div>
    ) : null;
  const table = {
    cols: ['Position', 'Invested', 'Annual income', 'Yield on cost'],
    num: [false, true, true, true],
    rows: [
      ...y.positions.map((p) => [
        p.name,
        money(p.investedMinor, PLN),
        money(p.incomeMinor, PLN),
        `${(p.yield * 100).toFixed(2)}%`,
      ]),
      [
        'Total',
        money(y.investedMinor, PLN),
        money(y.incomeMinor, PLN),
        y.total === undefined ? '–' : `${(total * 100).toFixed(2)}%`,
      ],
    ],
  };
  const legend = <span className="muted">{PLN}</span>;
  if (!y.investedMinor)
    return {
      loading: false,
      error: false,
      sub: '',
      legend,
      table,
      body: (
        <>
          <Empty>
            <span>
              <b>Yield on cost</b> = yearly dividends &amp; interest ÷ money you invested.
              <br />
              Add positions with their cost in <b>Investment</b> (open it larger ↗).
            </span>
          </Empty>
          {detLine}
        </>
      ),
    };
  const items = [
    ...y.positions.map((p) => ({ k: p.name, y: p.yield * 100, tot: false })),
    { k: 'Total', y: total * 100, tot: true },
  ];
  const mx = Math.max(...items.map((i) => i.y), 0.01);
  return {
    loading: false,
    error: false,
    sub: `${(total * 100).toFixed(2)}% total`,
    legend,
    table,
    body: (
      <div className="hbars">
        {items.map((i) => (
          <YieldBar key={i.tot ? '\u0000total' : i.k} k={i.k} y={i.y} tot={i.tot} mx={mx} />
        ))}
        {detLine}
      </div>
    ),
  };
}

export const yieldChart: ChartDef = {
  key: 'o_yoc',
  title: 'Yield on cost (PLN)',
  use: (q, env) => useOverviewParts(q, env, yieldParts),
};

/** The FI/RE settings of the larger view, in percent (stored as basis points). */
function FireSettings() {
  const doc = useInvestments().data;
  const edit = useEditInvestments();
  if (!doc) return null;
  return (
    <div className="inv-grid sm">
      <label>
        Safe withdrawal rate %
        <NumIn
          value={doc.swrBp}
          label="Safe withdrawal rate %"
          onSave={(v) => {
            edit((d) => ({ ...d, swrBp: Math.max(50, Math.min(1000, v)) }));
          }}
        />
      </label>
      <label>
        Expected real return %
        <NumIn
          value={doc.realReturnBp}
          label="Expected real return %"
          onSave={(v) => {
            edit((d) => ({ ...d, realReturnBp: Math.max(-1000, Math.min(2000, v)) }));
          }}
        />
      </label>
    </div>
  );
}

export function fireParts(d: OverviewData, _cur: string, env: ChartEnv): ChartParts {
  const f = d.fire;
  const swr = f.swrBp / 10000;
  const save = f.yearlySavingsMinor;
  const r = f.realReturnBp / 100;
  const years =
    f.fiNumberMinor <= 0
      ? ''
      : f.assetsMinor >= f.fiNumberMinor
        ? 'You have reached your FI number.'
        : !f.reachable
          ? 'Not reachable at the current savings rate.'
          : null;
  const mult = (1 / swr).toFixed(0);
  return {
    loading: false,
    error: false,
    sub: `${mult} × annual expenses`,
    table: {
      cols: ['Item', 'Value'],
      num: [false, true],
      rows: [
        ['Annual expenses', money(f.annualExpensesMinor, PLN)],
        ['Safe withdrawal rate', `${(swr * 100).toFixed(1)}%`],
        ['FI number', money(f.fiNumberMinor, PLN)],
        ['Invested assets', money(f.assetsMinor, PLN)],
        ['Progress', `${(f.progress * 100).toFixed(1)}%`],
        ['Yearly savings', money(save, PLN)],
        ['Years to FI', f.reachable ? String(f.years) : '–'],
      ],
    },
    body: (
      <div className="fire">
        <div className="fire-n">{money(f.fiNumberMinor, PLN)}</div>
        <div className="hint">
          {mult} × annual expenses of {money(f.annualExpensesMinor, PLN)} (
          {f.months < 12 ? `annualised from ${f.months} month${f.months === 1 ? '' : 's'}` : 'last 12 months'}) ·{' '}
          {(swr * 100).toFixed(1)}% safe withdrawal rate
        </div>
        <div className="meter">
          <i style={{ width: `${Math.min(100, f.progress * 100).toFixed(1)}%` }} />
        </div>
        <div className="fire-p">
          <span>
            <b>{(f.progress * 100).toFixed(1)}%</b> reached
          </span>
          <span className="muted">{money(f.assetsMinor, PLN)} invested</span>
        </div>
        <div className="hint">
          {years ?? (
            <>
              ≈{' '}
              <b>
                {f.years} year{f.years === 1 ? '' : 's'}
              </b>{' '}
              to FI saving {money(Math.max(0, save), PLN)}/year at {r.toFixed(1)}% real return.
            </>
          )}
          {f.assetsMinor ? (
            ''
          ) : (
            <>
              {' '}
              Enter your investments in <b>Investment</b> to track progress.
            </>
          )}
        </div>
        {env.big && <FireSettings />}
      </div>
    ),
  };
}

export const fireChart: ChartDef = {
  key: 'o_fire',
  title: 'FI/RE number',
  use: (q, env) => useOverviewParts(q, env, fireParts),
};
