// Goals (Phase 20, ADR 0031): saving an amount by a deadline, or spending a percentage less on a
// category than the months before. The server works out the progress (GET /goals); the card draws
// each goal's bar with the pace marker (where a steady pace would be today) and opens the form to
// add, change or delete one. Goals don't follow the overview's month or filters.

import { useState, type ReactNode } from 'react';

import { useAccounts } from '../api/hooks/accounts';
import { useCreateGoal, useDeleteGoal, useGoals, useUpdateGoal, type Goal } from '../api/hooks/goals';
import { useCategories } from '../api/hooks/transactions';
import { money, whole } from '../charts/lib/money';
import { Empty, partsOf, type ChartDef, type ChartParts } from '../charts/types';
import { fmtDayMedium, todayISO } from '../lib/dates';
import { errorMessage } from '../lib/errors';
import { moneyInputText, parseMoneyInput, plural } from '../lib/format';
import { Button } from '../ui/Button';
import { IconButton } from '../ui/IconButton';
import { Select, TextInput } from '../ui/inputs';
import { Modal } from '../ui/Modal';
import { toast } from '../ui/toastStore';

type State = Goal['progress']['state'];

/** Status icon, label and colour (the budgets card's look). */
export const GOAL_STATE: Record<State, [icon: string, label: string, color: string]> = {
  on_track: ['✓', 'On track', 'var(--st-good)'],
  behind: ['!', 'Behind', 'var(--st-warn)'],
  done: ['✓', 'Done', 'var(--st-good)'],
  missed: ['▲', 'Missed', 'var(--st-crit)'],
  not_started: ['·', 'Not started', 'var(--muted)'],
  needs_attention: ['?', 'Needs attention', 'var(--st-warn)'],
};

const pct = (v: number) => `${Math.max(0, Math.min(100, v * 100)).toFixed(1)}%`;
const ratio = (a = 0, b = 0) => (b > 0 ? a / b : 0);

/** What the goal measures: "2 500 / 10 000" or "100 / 400 this month". */
function soFar(g: Goal): [now: number, of: number, pace: number] {
  const p = g.progress;
  return g.kind === 'save'
    ? [p.savedMinor ?? 0, g.targetMinor ?? 0, p.expectedMinor ?? 0]
    : [p.thisMonthMinor ?? 0, p.monthlyLimitMinor ?? 0, p.allowedSoFarMinor ?? 0];
}

function detail(g: Goal): string {
  const p = g.progress;
  const left = p.daysLeft > 0 ? `${plural(p.daysLeft, 'day')} left` : `until ${fmtDayMedium(g.deadline)}`;
  if (g.kind === 'save') return `${g.account ? `on ${g.account.name} · ` : ''}${left}`;
  if (!g.category) return 'its category was deleted — pick another';
  const kept = p.monthsPast ? ` · ${p.monthsKept ?? 0} of ${p.monthsPast} months kept` : '';
  return `${g.reducePct ?? 0} % less on ${g.category.name}${kept} · ${left}`;
}

function GoalRow({ g, onEdit }: { g: Goal; onEdit: () => void }) {
  const [icon, label, col] = GOAL_STATE[g.progress.state];
  const [now, of, pace] = soFar(g);
  const started = g.progress.state !== 'not_started';
  return (
    <div className={g.status === 'active' ? 'gr' : 'gr closed'}>
      <span className="gn">
        {g.ai && (
          <span className="tag ai" title="Set by the assistant (Settings → Activity lists it, with Revert)">
            ✦
          </span>
        )}{' '}
        <b>{g.name}</b>
        <span className="gd-t">{detail(g)}</span>
      </span>
      <span className="bbar">
        <span className="bfill" style={{ width: pct(ratio(now, of)), background: col }} />
        {started && g.status === 'active' && <span className="bpace" style={{ left: pct(ratio(pace, of)) }} />}
      </span>
      <span className="bv">
        {whole(now, g.currency)} / {whole(of, g.currency)}
        {g.kind === 'reduce' && ' this month'}
      </span>
      <span className="bst">
        <b style={{ color: col }}>{icon}</b>{' '}
        {g.status === 'active' ? label : g.status === 'done' ? 'Closed' : 'Archived'}
      </span>
      <IconButton icon="pen" size="sm" title={`Change ${g.name}`} onClick={onEdit} />
    </div>
  );
}

function GoalsBody({ goals }: { goals: Goal[] }) {
  const [edit, setEdit] = useState<Goal | 'new' | null>(null);
  const shown = goals.filter((g) => g.status !== 'archived');
  return (
    <div className="bud goals">
      {shown.length === 0 && <Empty>No goals yet — save for something, or spend less on a category.</Empty>}
      {shown.map((g) => (
        <GoalRow
          key={g.id}
          g={g}
          onEdit={() => {
            setEdit(g);
          }}
        />
      ))}
      <div className="goals-add">
        <Button
          size="sm"
          icon="plus"
          onClick={() => {
            setEdit('new');
          }}
        >
          New goal
        </Button>
      </div>
      {edit && (
        <GoalDialog
          goal={edit === 'new' ? undefined : edit}
          onClose={() => {
            setEdit(null);
          }}
        />
      )}
    </div>
  );
}

export function goalsParts(goals: Goal[]): ChartParts {
  const open = goals.filter((g) => g.status === 'active').length;
  const archived = goals.filter((g) => g.status === 'archived').length;
  return {
    loading: false,
    error: false,
    sub: goals.length
      ? `${open} active${archived ? ` · ${archived} archived` : ''}`
      : 'save for something or spend less',
    body: <GoalsBody goals={goals} />,
    table: {
      cols: ['Goal', 'Kind', 'So far', 'Target', 'Deadline', 'Status'],
      num: [false, false, true, true, false, false],
      rows: goals.map((g) => {
        const [now, of] = soFar(g);
        return [
          g.name,
          g.kind === 'save' ? 'Save' : `Spend ${g.reducePct ?? 0} % less on ${g.category?.name ?? '?'}`,
          money(now, g.currency),
          g.kind === 'save' ? money(of, g.currency) : `${money(of, g.currency)} a month`,
          g.deadline,
          g.status === 'active' ? GOAL_STATE[g.progress.state][1] : g.status === 'done' ? 'Closed' : 'Archived',
        ];
      }),
    },
  };
}

export const goalsChart: ChartDef = {
  key: 'o_goals',
  title: 'Goals',
  use: () => partsOf(useGoals(), goalsParts),
};

const BASELINES = [1, 2, 3, 6, 12] as const;

/** Add a goal, or change or delete one. */
export function GoalDialog({ goal, onClose }: { goal?: Goal | undefined; onClose: () => void }) {
  const accounts = useAccounts().data ?? [];
  const cats = useCategories().data ?? [];
  const create = useCreateGoal();
  const update = useUpdateGoal();
  const remove = useDeleteGoal();
  const [kind, setKind] = useState<Goal['kind']>(goal?.kind ?? 'save');
  const [name, setName] = useState(goal?.name ?? '');
  const [accountId, setAccountId] = useState(goal?.account?.id ?? '');
  const currency =
    accounts.find((a) => a.id === accountId)?.currency ?? goal?.currency ?? accounts[0]?.currency ?? 'PLN';
  const [target, setTarget] = useState(goal?.targetMinor ? moneyInputText(goal.targetMinor, goal.currency) : '');
  const [categoryId, setCategoryId] = useState(goal?.category?.id ?? '');
  const [reducePct, setReducePct] = useState(String(goal?.reducePct ?? 20));
  const [baseline, setBaseline] = useState(String(goal?.baselineMonths ?? 3));
  const [startsOn, setStartsOn] = useState(todayISO());
  const [deadline, setDeadline] = useState(goal?.deadline ?? '');
  const [status, setStatus] = useState<Goal['status']>(goal?.status ?? 'active');
  const [error, setError] = useState('');
  const pending = create.isPending || update.isPending || remove.isPending;

  const submit = () => {
    const n = name.trim();
    const targetMinor = parseMoneyInput(target, currency);
    const pctN = Number(reducePct.replace('%', '').trim());
    const problem = !n
      ? 'Give the goal a name.'
      : kind === 'save' && (targetMinor === null || targetMinor <= 0)
        ? 'Type the amount to put aside, like 10 000.'
        : kind === 'reduce' && !categoryId
          ? 'Pick the category to spend less on.'
          : kind === 'reduce' && !(Number.isInteger(pctN) && pctN >= 1 && pctN <= 100)
            ? 'Type how much less to spend, a whole percentage from 1 to 100.'
            : !/^\d{4}-\d{2}-\d{2}$/.test(deadline)
              ? 'Pick the last day of the goal.'
              : '';
    if (problem) {
      setError(problem);
      return;
    }
    const fields =
      kind === 'save'
        ? { targetMinor: targetMinor ?? 0, baselineMonths: 3, ...(accountId ? { accountId } : {}) }
        : { categoryId, reducePct: pctN, baselineMonths: Number(baseline) };
    const fail = (e: unknown) => {
      setError(errorMessage(e));
    };
    if (!goal) {
      create.mutate(
        { name: n, kind, deadline, startsOn, ...fields, ...(kind === 'save' && !accountId ? { currency } : {}) },
        {
          onSuccess: () => {
            toast(`Added the goal ${n}`);
            onClose();
          },
          onError: fail,
        },
      );
      return;
    }
    update.mutate(
      { id: goal.id, body: { name: n, deadline, status, version: goal.version, ...fields } },
      {
        onSuccess: () => {
          toast(`Saved ${n}`);
          onClose();
        },
        onError: fail,
      },
    );
  };

  const field = (label: string, input: ReactNode) => (
    <label className="st-col">
      {label}
      {input}
    </label>
  );
  return (
    <Modal
      open
      onClose={onClose}
      title={goal ? 'Change the goal' : 'New goal'}
      size="sm"
      actions={
        <>
          {goal && (
            <Button
              variant="danger"
              disabled={pending}
              onClick={() => {
                remove.mutate(goal.id, {
                  onSuccess: () => {
                    toast(`Deleted the goal ${goal.name}`);
                    onClose();
                  },
                  onError: (e) => {
                    setError(errorMessage(e));
                  },
                });
              }}
            >
              Delete
            </Button>
          )}
          <span className="spacer" />
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" disabled={pending} onClick={submit}>
            {goal ? 'Save' : 'Add'}
          </Button>
        </>
      }
    >
      <form
        className="adm-form cash-form"
        onSubmit={(e) => {
          e.preventDefault();
          submit();
        }}
      >
        {!goal && (
          <div className="cash-kind" role="radiogroup" aria-label="Kind">
            {(['save', 'reduce'] as const).map((k) => (
              <label key={k} className={kind === k ? 'on' : undefined}>
                <input
                  type="radio"
                  name="goal-kind"
                  checked={kind === k}
                  onChange={() => {
                    setKind(k);
                  }}
                />
                {k === 'save' ? 'Save an amount' : 'Spend less on a category'}
              </label>
            ))}
          </div>
        )}
        {field(
          'Name',
          <TextInput
            autoFocus
            maxLength={80}
            placeholder={kind === 'save' ? 'Holiday in Greece' : 'Less eating out'}
            value={name}
            onChange={(e) => {
              setName(e.target.value);
            }}
          />,
        )}
        {kind === 'save' ? (
          <div className="cash-row">
            {field(
              `Amount (${currency})`,
              <TextInput
                mono
                inputMode="decimal"
                placeholder="10 000"
                value={target}
                onChange={(e) => {
                  setTarget(e.target.value);
                }}
              />,
            )}
            {field(
              'Counted on',
              <Select
                value={accountId}
                options={[
                  { value: '', label: 'Every account (income − spending)' },
                  ...accounts.map((a) => ({ value: a.id, label: a.name })),
                ]}
                onChange={setAccountId}
              />,
            )}
          </div>
        ) : (
          <>
            {field(
              'Category',
              <Select
                value={categoryId}
                options={[
                  { value: '', label: 'Pick a category' },
                  ...cats.map((c) => ({ value: c.id, label: c.name })),
                ]}
                onChange={setCategoryId}
              />,
            )}
            <div className="cash-row">
              {field(
                'Spend less by (%)',
                <TextInput
                  mono
                  inputMode="numeric"
                  value={reducePct}
                  onChange={(e) => {
                    setReducePct(e.target.value);
                  }}
                />,
              )}
              {field(
                'Than the average of',
                <Select
                  value={baseline}
                  options={BASELINES.map((m) => ({ value: String(m), label: `the last ${plural(m, 'month')}` }))}
                  onChange={setBaseline}
                />,
              )}
            </div>
          </>
        )}
        <div className="cash-row">
          {!goal &&
            field(
              'Starts',
              <TextInput
                type="date"
                value={startsOn}
                onChange={(e) => {
                  setStartsOn(e.target.value);
                }}
              />,
            )}
          {field(
            'Deadline',
            <TextInput
              type="date"
              value={deadline}
              onChange={(e) => {
                setDeadline(e.target.value);
              }}
            />,
          )}
          {goal &&
            field(
              'Status',
              <Select
                value={status}
                options={[
                  { value: 'active', label: 'Active' },
                  { value: 'done', label: 'Closed' },
                  { value: 'archived', label: 'Archived' },
                ]}
                onChange={setStatus}
              />,
            )}
        </div>
        <p className="set-err" role="alert">
          {error}
        </p>
      </form>
    </Modal>
  );
}
