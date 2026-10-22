// The import window at /imports/:importId (PLAN §7.10, port of the legacy v7 import settings
// window): settings bar, raw lines with header picking, column meanings, the preview table and a
// footer with the required fields, the file facts and the commit buttons.
//
// - previewed: every change re-stages on the server; "Import N transactions" commits.
// - committed: the stored rows and settings; "Reprocess" applies changed settings.
// - failed: the error and "Try again" (commit again).

import { useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { useNavigate, useParams } from 'react-router';

import { invalidateData } from '../api/hooks/accounts';
import {
  commitImport,
  deleteImport,
  getImport,
  reprocessImport,
  type ImportDetail,
  type ImportPreview,
} from '../api/hooks/imports';
import { fmtDay } from '../lib/dates';
import { errorMessage } from '../lib/errors';
import { fmtInt, fmtSize, plural } from '../lib/format';
import { TableView } from '../shell/Views';
import { Button } from '../ui/Button';
import { Checkbox } from '../ui/inputs';
import { Modal } from '../ui/Modal';
import { toast } from '../ui/toastStore';
import { effective, type Effective } from './editor';
import { delimiterLabel, ImportBar } from './ImportBar';
import { MappingEditor } from './MappingEditor';
import { useImportModel } from './model';
import { PreviewTable } from './PreviewTable';
import { committedMessage, settle, stepOf, useImportQueue, waitForCommit } from './queue';
import { RawLines } from './RawLines';
import { requiredChecks } from './roles';

/** /imports/:importId: the window over the transactions view. */
export function ImportRoute() {
  const { importId = '' } = useParams();
  return (
    <>
      <TableView />
      <ImportWindow key={importId} id={importId} />
    </>
  );
}

type Busy = null | 'committing' | 'reprocessing' | 'cancelling';

export function ImportWindow({ id }: { id: string }) {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const { detail, mode, editor, state } = useImportModel(id);
  const step = useImportQueue((s) => stepOf(s.items, id));
  const [remember, setRemember] = useState(true);
  const [busy, setBusy] = useState<Busy>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const [mapOpen, setMapOpen] = useState(false);

  const p = state.preview;
  const eff = effective(state);
  const status = detail.data?.status;
  const live = mode === 'preview';
  const failed = status === 'failed' || failure !== null;

  const leave = (accountId?: string) => {
    void navigate(`/t/${accountId ?? 'all'}`);
  };

  const finish = (imp: ImportDetail | null, verb: 'Imported' | 'Reprocessed') => {
    if (imp?.status === 'committed') {
      invalidateData(qc);
      toast(verb === 'Imported' ? committedMessage(imp) : committedMessage(imp).replace(/^Imported/, 'Reprocessed:'));
      settle(id, 'done');
      leave(imp.accountId);
      return;
    }
    void qc.invalidateQueries({ queryKey: ['import', id] });
    setFailure(imp?.error ?? 'The import did not finish in time. Try again.');
    setBusy(null);
  };

  const commit = async () => {
    if (!editor) return;
    setFailure(null);
    setBusy('committing');
    try {
      await editor.flush();
      if (editor.getState().problem) {
        setBusy(null);
        return;
      }
      await commitImport(id, remember);
      finish(await waitForCommit({ getImport }, id), 'Imported');
    } catch (e) {
      setFailure(errorMessage(e));
      setBusy(null);
    }
  };

  const reprocess = async () => {
    if (!eff) return;
    setFailure(null);
    setBusy('reprocessing');
    try {
      await reprocessImport(id, { mapping: eff.mapping, options: eff.options });
      finish(await waitForCommit({ getImport }, id), 'Reprocessed');
    } catch (e) {
      setFailure(errorMessage(e));
      setBusy(null);
    }
  };

  const cancel = async () => {
    if (status !== 'previewed') {
      settle(id, 'skipped');
      leave(detail.data?.accountId);
      return;
    }
    setBusy('cancelling');
    try {
      await deleteImport(id);
      invalidateData(qc);
    } catch {
      // the preview stays in the history; closing the window still works
    }
    settle(id, 'skipped');
    leave(eff?.accountId);
  };

  const title = mode === 'stored' ? 'Import settings' : 'Import';
  const sub = (
    <>
      {detail.data?.filename}
      {detail.data && ` · ${fmtSize(detail.data.sizeBytes)}`}
      {step && ` · file ${step}`}
    </>
  );

  return (
    <Modal open onClose={() => void cancel()} title={title} sub={sub} dismissOnBackdrop={false}>
      {detail.isError && (
        <div className="imp-b">
          <div className="conn-err">{errorMessage(detail.error)}</div>
        </div>
      )}
      {p && eff ? (
        <>
          <ImportBar
            preview={p}
            eff={eff}
            lockAccount={mode === 'stored'}
            readOnly={status === 'failed' || busy !== null}
            onChange={(ch) => {
              editor?.change(ch);
            }}
          />
          <div className="imp-b">
            {failed && (
              <div className="conn-err" role="alert">
                {failure ?? detail.data?.error ?? 'The import failed.'}{' '}
                <Button size="sm" onClick={() => void commit()} disabled={busy !== null || status === 'committed'}>
                  Try again
                </Button>
              </div>
            )}
            {state.problem && (
              <div className="conn-err" role="alert">
                {state.problem.errors?.length
                  ? state.problem.errors.map((e) => e.message).join(' · ')
                  : (state.problem.detail ?? state.problem.title)}
              </div>
            )}
            {p.warnings.map((w) => (
              <div key={w.code} className="conn-err">
                {w.message}
              </div>
            ))}
            {mode === 'stored' && status === 'committed' && (
              <div className="hint">
                Changes apply when you reprocess. Manual edits survive wherever a transaction stays the same.
              </div>
            )}
            <RawLines
              importId={id}
              options={p.import.options}
              headerAt={p.stats.headerAt}
              disabled={status === 'failed' || busy !== null}
              onOptions={(options) => {
                editor?.change({ options });
              }}
            />
            <div className="imp-sec">
              <div className="lbl">
                <button
                  type="button"
                  className="link"
                  aria-expanded={mapOpen || p.missing.length > 0}
                  onClick={() => {
                    setMapOpen(!mapOpen);
                  }}
                >
                  {mapOpen || p.missing.length > 0 ? '▾' : '▸'} Column meanings
                </button>
              </div>
              {(mapOpen || p.missing.length > 0) && (
                <MappingEditor
                  preview={p}
                  mapping={eff.mapping}
                  options={eff.options}
                  live={live}
                  disabled={status === 'failed' || busy !== null}
                  errors={state.problem?.errors ?? []}
                  onMapping={(mapping, options) => {
                    editor?.change({ mapping, options });
                  }}
                />
              )}
            </div>
            <PreviewTable
              importId={id}
              preview={p}
              mapping={eff.mapping}
              options={eff.options}
              live={live}
              disabled={status === 'failed' || busy !== null}
              onMapping={(mapping, options) => {
                editor?.change({ mapping, options });
              }}
            />
          </div>
          <Footer
            preview={p}
            eff={eff}
            mode={mode}
            status={status}
            busy={busy}
            saving={state.saving || state.draft !== null}
            remember={remember}
            onRemember={setRemember}
            onCancel={() => void cancel()}
            onCommit={() => void commit()}
            onReprocess={() => void reprocess()}
          />
        </>
      ) : (
        !detail.isError && (
          <div className="imp-b">
            <p className="muted">
              {state.problem ? (state.problem.detail ?? state.problem.title) : 'Reading the file…'}
            </p>
          </div>
        )
      )}
    </Modal>
  );
}

function Footer({
  preview: p,
  eff,
  mode,
  status,
  busy,
  saving,
  remember,
  onRemember,
  onCancel,
  onCommit,
  onReprocess,
}: {
  preview: ImportPreview;
  eff: Effective;
  mode: 'preview' | 'stored' | null;
  status: ImportDetail['status'] | undefined;
  busy: Busy;
  saving: boolean;
  remember: boolean;
  onRemember: (v: boolean) => void;
  onCancel: () => void;
  onCommit: () => void;
  onReprocess: () => void;
}) {
  const s = p.stats;
  const imp = p.import;
  const importable = Math.max(0, s.rows - s.errors);
  const ready = !!eff.accountId && p.missing.length === 0 && importable > 0;
  const facts = [
    imp.encoding,
    delimiterLabel(imp.delimiter),
    s.headerAt ? `header at row ${String(s.headerAt)}` : 'no header row',
    plural(s.rows, 'row'),
    ...(s.dropped ? [`${plural(s.dropped, 'summary row')} skipped`] : []),
    ...(s.errors ? [plural(s.errors, 'error')] : []),
    ...(mode === 'preview' && eff.accountId
      ? [`${fmtInt(s.newEstimate)} new`, plural(s.duplicatesEstimate, 'duplicate')]
      : []),
    ...(s.dateFrom && s.dateTo ? [`${fmtDay(s.dateFrom)} – ${fmtDay(s.dateTo)}`] : []),
  ];
  return (
    <div className="imp-f">
      <span className="hint">
        <span className="imp-req">
          {requiredChecks(eff.mapping, eff.options).map((c) => (
            <span key={c.label} className={c.ok ? 'ok' : c.required ? 'miss' : undefined}>
              {c.ok ? '✓' : '!'} {c.label}
            </span>
          ))}
        </span>
        <span>{facts.join(' · ')}</span>
      </span>
      <span className="spacer" />
      {mode === 'preview' && (
        <Checkbox checked={remember} onChange={onRemember} label="Remember this mapping for files like this" />
      )}
      <Button variant="ghost" onClick={onCancel} disabled={busy !== null}>
        {status === 'previewed' ? 'Cancel' : 'Close'}
      </Button>
      {mode === 'preview' && (
        <Button
          variant="primary"
          onClick={onCommit}
          disabled={!ready || busy !== null || saving}
          title={
            ready
              ? undefined
              : !eff.accountId
                ? 'Choose or create an account first'
                : 'Map the date and amount columns first'
          }
        >
          {busy === 'committing' ? 'Importing…' : `Import ${plural(importable, 'transaction')}`}
        </Button>
      )}
      {mode === 'stored' && status === 'committed' && (
        <Button variant="primary" onClick={onReprocess} disabled={busy !== null || !ready}>
          {busy === 'reprocessing' ? 'Reprocessing…' : 'Reprocess'}
        </Button>
      )}
    </div>
  );
}
