// Import history in the sidebar (PLAN task 9.5): an account's imports under it, newest first, with
// status, new/duplicate counts and the source label (b1, b2 … when files come from different
// layouts). Each import has a menu: Download original, Re-map and reprocess, Delete.

import { useState, type MouseEvent } from 'react';
import { useNavigate } from 'react-router';

import { importFileUrl, useDeleteImport, type ImportSummary } from '../api/hooks/imports';
import { fmtDateTime } from '../lib/dates';
import { errorMessage } from '../lib/errors';
import { fmtInt } from '../lib/format';
import { Button } from '../ui/Button';
import { Icon } from '../ui/Icon';
import { IconButton } from '../ui/IconButton';
import { Menu, MenuItem } from '../ui/Menu';
import { Modal } from '../ui/Modal';
import { toast } from '../ui/toastStore';

const STATUS: Record<ImportSummary['status'], string | null> = {
  previewed: 'preview',
  committing: 'importing…',
  committed: null,
  failed: 'failed',
};

function counts(i: ImportSummary): string {
  const n = Number(i.stats['new'] ?? 0);
  const d = Number(i.stats['duplicates'] ?? 0);
  if (i.status !== 'committed') return fmtInt(Number(i.stats['rows'] ?? 0));
  return d ? `+${fmtInt(n)} · ${fmtInt(d)} dup` : `+${fmtInt(n)}`;
}

export function ImportHistory({ imports }: { imports: readonly ImportSummary[] }) {
  const navigate = useNavigate();
  const [menu, setMenu] = useState<{ imp: ImportSummary; anchor: HTMLElement } | null>(null);
  const [confirm, setConfirm] = useState<ImportSummary | null>(null);
  const open = (e: MouseEvent<HTMLElement>, imp: ImportSummary) => {
    e.preventDefault();
    e.stopPropagation();
    setMenu({ imp, anchor: e.currentTarget });
  };
  if (!imports.length) return <div className="fi-empty imp">No imports</div>;
  return (
    <div role="list" className="imp-list">
      {imports.map((i) => {
        const badge = STATUS[i.status];
        return (
          <div
            key={i.id}
            role="listitem"
            className={i.status === 'failed' ? 'fi imp err' : 'fi imp'}
            title={`${i.filename} · imported ${fmtDateTime(i.committedAt ?? i.createdAt)}${i.error ? ` · ${i.error}` : ''}`}
            onClick={() => void navigate(`/imports/${i.id}`)}
            onContextMenu={(e) => {
              open(e, i);
            }}
          >
            <Icon name="file" />
            <span className="fn">{i.filename}</span>
            {i.profileLabel && (
              <span
                className="tag src"
                title={`Import profile ${i.profileLabel}: files with the same columns share a label`}
              >
                {i.profileLabel}
              </span>
            )}
            {badge && <span className={`tag st-${i.status}`}>{badge}</span>}
            <span className="fc">{counts(i)}</span>
            <IconButton
              icon="sliders"
              size="sm"
              title={`Actions for ${i.filename}`}
              aria-haspopup="menu"
              onClick={(e) => {
                open(e, i);
              }}
            />
          </div>
        );
      })}
      {menu && (
        <Menu
          open
          anchor={menu.anchor}
          onClose={() => {
            setMenu(null);
          }}
          label={`Actions for ${menu.imp.filename}`}
          width={250}
        >
          <MenuItem
            icon="download"
            onSelect={() => {
              window.location.assign(importFileUrl(menu.imp.id));
              setMenu(null);
            }}
          >
            Download original
          </MenuItem>
          <MenuItem
            icon="pen"
            disabled={menu.imp.status === 'committing'}
            onSelect={() => {
              void navigate(`/imports/${menu.imp.id}`);
              setMenu(null);
            }}
          >
            {menu.imp.status === 'previewed' ? 'Open preview' : 'Re-map and reprocess'}
          </MenuItem>
          <MenuItem
            icon="x"
            disabled={menu.imp.status === 'committing'}
            onSelect={() => {
              setConfirm(menu.imp);
              setMenu(null);
            }}
          >
            Delete…
          </MenuItem>
        </Menu>
      )}
      <DeleteImport
        imp={confirm}
        onClose={() => {
          setConfirm(null);
        }}
      />
    </div>
  );
}

function DeleteImport({ imp, onClose }: { imp: ImportSummary | null; onClose: () => void }) {
  const del = useDeleteImport();
  return (
    <Modal open={imp !== null} onClose={onClose} title="Delete import" size="sm">
      <div className="modal-form">
        <p>
          Delete the import of <b>{imp?.filename}</b>? Its transactions are removed, except the ones another import of
          this account also contains — those are kept.
        </p>
        <div className="set-actions">
          <Button onClick={onClose}>Cancel</Button>
          <Button
            variant="danger"
            disabled={del.isPending}
            onClick={() => {
              if (!imp) return;
              del.mutate(imp.id, {
                onSuccess: () => {
                  toast(`Deleted the import of ${imp.filename}`);
                  onClose();
                },
                onError: (e) => {
                  toast(errorMessage(e), { kind: 'error' });
                },
              });
            }}
          >
            Delete import
          </Button>
        </div>
      </div>
    </Modal>
  );
}
