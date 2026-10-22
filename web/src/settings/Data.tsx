import { useState } from 'react';
import { useNavigate } from 'react-router';

import { useDeleteMe } from '../api/hooks/me';
import { errorMessage } from '../lib/errors';
import { resetStores } from '../stores/persist';
import { Button } from '../ui/Button';
import { TextInput } from '../ui/inputs';
import { Modal } from '../ui/Modal';
import { toast } from '../ui/toastStore';

/** GET /me/export is a plain download: same-origin cookie, no CSRF header needed for GET. */
export const EXPORT_URL = '/api/v1/me/export';

export function DataSettings() {
  const [confirming, setConfirming] = useState(false);
  return (
    <>
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
            size="sm"
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
