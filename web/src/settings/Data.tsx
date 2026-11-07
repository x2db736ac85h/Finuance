import { useRef, useState } from 'react';
import { useNavigate } from 'react-router';

import { useLegacyImport, useLegacyImports } from '../api/hooks/legacy';
import { useDeleteMe } from '../api/hooks/me';
import { LegacyWizard, Running } from '../legacy/LegacyWizard';
import { fmtDateTime } from '../lib/dates';
import { errorMessage } from '../lib/errors';
import { resetStores } from '../stores/persist';
import { Button } from '../ui/Button';
import { TextInput } from '../ui/inputs';
import { Modal } from '../ui/Modal';
import { toast } from '../ui/toastStore';

/** GET /me/export is a plain download: same-origin cookie, no CSRF header needed for GET. */
export const EXPORT_URL = '/api/v1/me/export';

const RUN_STATUS = {
  queued: 'waiting to start',
  running: 'running',
  done: 'finished',
  failed: 'stopped with an error',
};

export function DataSettings() {
  const [confirming, setConfirming] = useState(false);
  const [backup, setBackup] = useState<File | null>(null);
  const [shown, setShown] = useState<string | null>(null);
  const picker = useRef<HTMLInputElement>(null);
  const runs = useLegacyImports();
  const last = runs.data?.[0];
  return (
    <>
      <div className="set-card">
        <div className="lbl">Import from the single-file app</div>
        <p className="hint">
          Moves everything from a backup of the old single-file Finuance (finuance-backup-….json): the imported files,
          groups and labels, budgets, the opening balance, duplicate decisions, investments and chats. A locked backup
          is unlocked here in your browser; the passphrase is never sent.
        </p>
        {last && (
          <p className="st-line">
            Last migration {fmtDateTime(last.createdAt)}: {RUN_STATUS[last.status]}.{' '}
            <button
              type="button"
              className="link"
              onClick={() => {
                setShown(last.id);
              }}
            >
              {last.status === 'done' || last.status === 'failed' ? 'View report' : 'Show progress'}
            </button>
          </p>
        )}
        <div className="set-actions">
          <input
            ref={picker}
            type="file"
            accept=".json,application/json"
            hidden
            aria-label="Backup file"
            onChange={(e) => {
              setBackup(e.target.files?.[0] ?? null);
              e.target.value = '';
            }}
          />
          <Button
            icon="upload"
            onClick={() => {
              picker.current?.click();
            }}
          >
            Choose backup…
          </Button>
        </div>
      </div>
      <LegacyWizard
        file={backup}
        onClose={() => {
          setBackup(null);
        }}
      />
      <RunWindow
        id={shown}
        onClose={() => {
          setShown(null);
        }}
      />
      <div className="set-card">
        <div className="lbl">Download your data</div>
        <p className="hint">
          A zip file with every account, import, transaction, category and setting, plus the original CSV files.
        </p>
        <div className="set-actions">
          <a className="btn sm" href={EXPORT_URL} download>
            Download export
          </a>
        </div>
      </div>
      <div className="set-card">
        <div className="lbl">Delete account</div>
        <p className="hint">
          Deletes your account and all of its data from this server. This can't be undone, so download an export first
          if you may need the data again.
        </p>
        <div className="set-actions">
          <Button
            variant="danger"
            onClick={() => {
              setConfirming(true);
            }}
          >
            Delete account…
          </Button>
        </div>
      </div>
      <DeleteDialog
        open={confirming}
        onClose={() => {
          setConfirming(false);
        }}
      />
    </>
  );
}

function RunWindow({ id, onClose }: { id: string | null; onClose: () => void }) {
  const run = useLegacyImport(id);
  return (
    <Modal open={id !== null} onClose={onClose} title="Migration from the single-file app" size="md">
      <div className="legacy">
        <Running run={run.data} error={run.error} onClose={onClose} />
      </div>
    </Modal>
  );
}

function DeleteDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const del = useDeleteMe();
  const navigate = useNavigate();
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const close = () => {
    setPassword('');
    setError('');
    onClose();
  };
  return (
    <Modal open={open} onClose={close} title="Delete account" size="sm">
      <form
        className="modal-form"
        onSubmit={(e) => {
          e.preventDefault();
          setError('');
          del.mutate(password, {
            onSuccess: () => {
              resetStores();
              toast('Deleted your account and all of its data');
              void navigate('/login', { replace: true });
            },
            onError: (err) => {
              setError(errorMessage(err));
            },
          });
        }}
      >
        <p>Enter your password to delete your account and all of its data. This can't be undone.</p>
        <input type="text" autoComplete="username" hidden readOnly />
        <TextInput
          type="password"
          aria-label="Password"
          placeholder="Password"
          autoComplete="current-password"
          autoFocus
          value={password}
          onChange={(e) => {
            setPassword(e.target.value);
          }}
        />
        <div className="set-actions">
          <span className="set-err" role="alert">
            {error}
          </span>
          <Button onClick={close}>Cancel</Button>
          <Button type="submit" variant="danger" disabled={!password || del.isPending}>
            Delete account
          </Button>
        </div>
      </form>
    </Modal>
  );
}
