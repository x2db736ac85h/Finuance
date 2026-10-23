// Settings → Data → Import from the single-file app (PLAN §4.10, task 14.1): read the backup,
// unlock it, choose the accounts, start the migration, follow it and show its report.

import { useEffect, useMemo, useState } from 'react';

import { useAccounts, type Account } from '../api/hooks/accounts';
import { useAssistantSettings } from '../api/hooks/assistant';
import { useLegacyImport, useStartLegacyImport, type LegacyImport } from '../api/hooks/legacy';
import { fmtInt, fmtMoney, fmtSize, plural } from '../lib/format';
import { errorMessage } from '../lib/errors';
import { fmtDay } from '../lib/dates';
import { parsePrefs } from '../stores/prefs';
import { NARROW_PX, useUi } from '../stores/uiStore';
import { Button } from '../ui/Button';
import { Checkbox, Select, TextInput, type Option } from '../ui/inputs';
import { Modal } from '../ui/Modal';
import {
  buildManifest,
  fileInfo,
  isLocked,
  legacyPrefs,
  openBackup,
  parseBackup,
  summary,
  WrongPassphrase,
  type Backup,
  type FileInfo,
  type Opened,
  type Target,
} from './backup';
import { LegacyReportView } from './LegacyReport';

/** The wizard's starting plan: the account of each file and the names of the new accounts. */
export type Plan = { names: string[]; picks: Record<string, string> }; // pick: "new:<i>" | "id:<uuid>" | "skip"

/**
 * A file whose account number ends like an existing account's number goes there; the others get
 * one new account per file layout (the legacy b1, b2…), named Account 1, Account 2, …
 */
export function defaultPlan(o: Opened, infos: Record<string, FileInfo>, accounts: Account[]): Plan {
  const names: string[] = [];
  const layouts = new Map<string, number>();
  const picks: Record<string, string> = {};
  for (const f of o.files) {
    const info = infos[f.id];
    const match = accounts.find((a) => a.numberHint && info?.accountHints.includes(a.numberHint));
    if (match) {
      picks[f.id] = `id:${match.id}`;
      continue;
    }
    const layout = info?.layout ?? f.id;
    let k = layouts.get(layout);
    if (k === undefined) {
      k = names.length;
      layouts.set(layout, k);
      names.push(`Account ${String(k + 1)}`);
    }
    picks[f.id] = `new:${String(k)}`;
  }
  return { names, picks };
}

function target(pick: string, names: string[]): Target | null {
  if (pick.startsWith('id:')) return { id: pick.slice(3) };
  if (pick.startsWith('new:')) return { new: names[Number(pick.slice(4))] ?? '' };
  return null;
}

type Step = 'unlock' | 'review' | 'running';

type Props = { file: File | null; onClose: () => void };

export function LegacyWizard({ file, onClose }: Props) {
  return file ? <Wizard key={file.name + String(file.lastModified)} file={file} onClose={onClose} /> : null;
}

function Wizard({ file, onClose }: { file: File; onClose: () => void }) {
  const [backup, setBackup] = useState<Backup | null>(null);
  const [opened, setOpened] = useState<Opened | null>(null);
  const [error, setError] = useState('');
  const [step, setStep] = useState<Step | null>(null);
  const [runId, setRunId] = useState<string | null>(null);

  useEffect(() => {
    let live = true;
    file
      .text()
      .then(async (t) => {
        const b = parseBackup(t);
        const o = isLocked(b) ? null : await openBackup(b);
        if (!live) return;
        setBackup(b);
        setOpened(o);
        setStep(o ? 'review' : 'unlock');
      })
      .catch((e: unknown) => {
        if (live) setError(e instanceof Error ? e.message : 'The file could not be read.');
      });
    return () => {
      live = false;
    };
  }, [file]);
  const run = useLegacyImport(runId);
  const busy = run.data?.status === 'queued' || run.data?.status === 'running';

  return (
    <Modal
      open
      onClose={onClose}
      title="Import from the single-file app"
      size="md"
      sub={file.name}
      dismissOnBackdrop={!busy && step !== 'review'}
    >
      <div className="legacy">
        {error && !step && (
          <>
            <p className="set-err" role="alert">
              {error}
            </p>
            <div className="set-actions">
              <Button onClick={onClose}>Close</Button>
            </div>
          </>
        )}
        {!error && !step && <p className="hint">Reading the backup…</p>}
        {step === 'unlock' && backup && (
          <Unlock
            backup={backup}
            onOpen={(o) => {
              setOpened(o);
              setStep('review');
            }}
            onCancel={onClose}
          />
        )}
        {step === 'review' && opened && (
          <Review
            opened={opened}
            onCancel={onClose}
            onStarted={(id) => {
              setRunId(id);
              setStep('running');
            }}
          />
        )}
        {step === 'running' && <Running run={run.data} error={run.error} onClose={onClose} />}
      </div>
    </Modal>
  );
}

function Unlock({ backup, onOpen, onCancel }: { backup: Backup; onOpen: (o: Opened) => void; onCancel: () => void }) {
  const [pass, setPass] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  return (
    <form
      className="modal-form"
      onSubmit={(e) => {
        e.preventDefault();
        setBusy(true);
        setError('');
        openBackup(backup, pass)
          .then(onOpen)
          .catch((err: unknown) => {
            setError(
              err instanceof WrongPassphrase
                ? 'The passphrase is wrong. Use the one you set in the single-file app (Settings → Lock).'
                : err instanceof Error
                  ? err.message
                  : 'The backup could not be opened.',
            );
          })
          .finally(() => {
            setBusy(false);
          });
      }}
    >
      <p>
        This backup is locked with a passphrase. It is checked and the data decrypted here in your browser; the
        passphrase is never sent anywhere.
      </p>
      <input type="text" autoComplete="username" hidden readOnly />
      <TextInput
        type="password"
        aria-label="Passphrase"
        placeholder="Passphrase"
        autoComplete="current-password"
        autoFocus
        value={pass}
        onChange={(e) => {
          setPass(e.target.value);
        }}
      />
      <div className="set-actions">
        <span className="set-err" role="alert">
          {error}
        </span>
        <Button onClick={onCancel}>Cancel</Button>
        <Button type="submit" variant="primary" disabled={!pass || busy}>
          {busy ? 'Unlocking…' : 'Unlock'}
        </Button>
      </div>
    </form>
  );
}

function retentionText(days: number | null | undefined): string {
  if (days === null) return 'until you delete them';
  const d = days ?? 2;
  return d === 1 ? 'for a day after their last message' : `for ${String(d)} days after their last message`;
}

function Review({
  opened,
  onCancel,
  onStarted,
}: {
  opened: Opened;
  onCancel: () => void;
  onStarted: (id: string) => void;
}) {
  const accounts = useAccounts();
  const settings = useAssistantSettings();
  const start = useStartLegacyImport();
  const infos = useMemo(() => Object.fromEntries(opened.files.map((f) => [f.id, fileInfo(f)])), [opened]);
  const [plan, setPlan] = useState<Plan | null>(null);
  const [openingPick, setOpeningPick] = useState<string | null>(null);
  const [chats, setChats] = useState(true);
  const [prefs, setPrefs] = useState(true);
  const [error, setError] = useState('');
  const sum = summary(opened);

  if (!plan && accounts.data) setPlan(defaultPlan(opened, infos, accounts.data));
  if (!plan) return <p className="hint">Loading your accounts…</p>;

  const options: Option<string>[] = [
    ...plan.names.map((n, i) => ({ value: `new:${String(i)}`, label: `New: ${n || '(no name)'}` })),
    ...(accounts.data ?? []).map((a) => ({ value: `id:${a.id}`, label: a.name })),
    { value: 'skip', label: "Don't import" },
  ];
  const used = [...new Set(Object.values(plan.picks).filter((p) => p !== 'skip'))];
  const usedOptions = options.filter((o) => used.includes(o.value));
  const opening = openingPick !== null && used.includes(openingPick) ? openingPick : used[0];
  const namesOk = plan.names.every((n, i) => !used.includes(`new:${String(i)}`) || (n.trim() !== '' && n.length <= 80));

  const submit = () => {
    setError('');
    const targets: Record<string, Target> = {};
    for (const [id, pick] of Object.entries(plan.picks)) {
      const t = target(pick, plan.names);
      if (t) targets[id] = t;
    }
    const built = buildManifest(opened, {
      targets,
      opening: sum.opening && opening ? target(opening, plan.names) : null,
      includeChats: chats,
    });
    start.mutate(built, {
      onSuccess: (r) => {
        if (prefs) {
          useUi.getState().hydrate(parsePrefs(legacyPrefs(opened.prefs)), window.innerWidth < NARROW_PX);
        }
        onStarted(r.id);
      },
      onError: (e) => {
        setError(errorMessage(e));
      },
    });
  };

  const parts = [
    plural(sum.files, 'file'),
    sum.groups && plural(sum.groups, 'group'),
    sum.labels && plural(sum.labels, 'label'),
    sum.budgets && plural(sum.budgets, 'budget'),
    sum.decisions && plural(sum.decisions, 'duplicate decision'),
    sum.investments && 'investments',
    sum.chats && plural(sum.chats, 'chat'),
  ].filter(Boolean);

  return (
    <div className="legacy-review">
      <p>
        The backup holds {parts.join(' · ')}. Choose the account each file goes to; each file is imported like a new
        upload, so overlapping files are merged as before.
      </p>
      {plan.names.length > 0 && (
        <div className="legacy-names">
          <div className="lbl">New accounts</div>
          {plan.names.map((n, i) => (
            <TextInput
              key={i}
              aria-label={`Name of new account ${String(i + 1)}`}
              value={n}
              maxLength={80}
              onChange={(e) => {
                const names = [...plan.names];
                names[i] = e.target.value;
                setPlan({ ...plan, names });
              }}
            />
          ))}
        </div>
      )}
      <table className="legacy-files">
        <thead>
          <tr>
            <th>File</th>
            <th className="r">Size</th>
            <th>Dates</th>
            <th>Account</th>
          </tr>
        </thead>
        <tbody>
          {opened.files.map((f) => {
            const info = infos[f.id];
            return (
              <tr key={f.id}>
                <td>{f.name}</td>
                <td className="r">{fmtSize(f.size)}</td>
                <td>{info?.from && info.to ? `${fmtDay(info.from)} – ${fmtDay(info.to)}` : '—'}</td>
                <td>
                  <Select
                    aria-label={`Account for ${f.name}`}
                    value={plan.picks[f.id] ?? 'skip'}
                    options={options}
                    onChange={(v) => {
                      setPlan({ ...plan, picks: { ...plan.picks, [f.id]: v } });
                    }}
                  />
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
      {sum.opening !== 0 && used.length > 0 && (
        <label className="legacy-row">
          Opening balance of {fmtMoney({ amountMinor: Math.round(sum.opening * 100), currency: 'PLN' })} goes to
          <Select
            value={opening ?? ''}
            options={usedOptions}
            onChange={(v) => {
              setOpeningPick(v);
            }}
          />
        </label>
      )}
      {sum.chats > 0 && (
        <div>
          <Checkbox checked={chats} onChange={setChats} label={`Include the ${plural(sum.chats, 'assistant chat')}`} />
          <p className="hint">
            They are kept {retentionText(settings.data?.retentionDays)}, like your other chats; you can change that in
            the assistant's settings.
          </p>
        </div>
      )}
      <Checkbox checked={prefs} onChange={setPrefs} label="Use the old app's appearance and chart settings" />
      <div className="set-actions">
        <span className="set-err" role="alert">
          {error}
        </span>
        <Button onClick={onCancel}>Cancel</Button>
        <Button variant="primary" disabled={used.length === 0 || !namesOk || start.isPending} onClick={submit}>
          Start migration
        </Button>
      </div>
    </div>
  );
}

const STEPS: Record<string, string> = {
  labels: 'Matching labels and duplicate decisions…',
  settings: 'Budgets, opening balance and investments…',
  chats: 'Copying the chats…',
};

/** A run's progress, then its report. */
export function Running({
  run,
  error,
  onClose,
}: {
  run: LegacyImport | undefined;
  error: Error | null;
  onClose: () => void;
}) {
  if (error) {
    return (
      <>
        <p className="set-err" role="alert">
          {errorMessage(error)}
        </p>
        <div className="set-actions">
          <Button onClick={onClose}>Close</Button>
        </div>
      </>
    );
  }
  if (!run || run.status === 'queued' || run.status === 'running') {
    const p = run?.progress;
    const total = Math.max(p?.filesTotal ?? 1, 1);
    const done = p?.step === 'files' ? p.filesDone : total;
    const text =
      !p || run.status === 'queued'
        ? 'Waiting for the server…'
        : p.step === 'files'
          ? `Importing ${p.current ?? 'files'} (${fmtInt(p.filesDone + 1)} of ${fmtInt(total)})…`
          : (STEPS[p.step] ?? 'Working…');
    return (
      <div className="legacy-progress" role="status">
        <p>{text}</p>
        <div className="bar">
          <i style={{ width: `${String(Math.round((done / total) * 100))}%` }} />
        </div>
        <p className="hint">You can close this window; the migration goes on and its report is kept under Data.</p>
      </div>
    );
  }
  return (
    <>
      <LegacyReportView run={run} />
      <div className="set-actions">
        <Button variant="primary" onClick={onClose}>
          Done
        </Button>
      </div>
    </>
  );
}
