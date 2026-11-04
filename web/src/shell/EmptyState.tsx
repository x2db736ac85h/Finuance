// The legacy empty state (`.empty #bigDrop`): the big drop zone with Browse and Load demo data,
// and three tips. Clicking the zone opens the file picker, as in the legacy app.

import { useNavigate } from 'react-router';

import { useLoadDemo } from '../api/hooks/accounts';
import { errorMessage } from '../lib/errors';
import { plural } from '../lib/format';
import { Button } from '../ui/Button';
import { Icon } from '../ui/Icon';
import { modKey } from '../ui/Kbd';
import { toast } from '../ui/toastStore';
import { useCashDialogs } from '../cash/store';
import { openFilePicker } from './files';

export function EmptyState() {
  const demo = useLoadDemo();
  const navigate = useNavigate();
  const loadDemo = () => {
    demo.mutate(undefined, {
      onSuccess: (d) => {
        toast(`Loaded demo data: ${plural(d.transactions, 'transaction')}`);
        void navigate(`/t/${d.accountId}`);
      },
      onError: (e) => {
        toast(errorMessage(e), { kind: 'error' });
      },
    });
  };
  return (
    <div className="empty">
      {/* The whole zone opens the picker; the buttons inside are the keyboard route. */}
      <div
        className="dz"
        onClick={(e) => {
          if (!(e.target as Element).closest('button, .tips')) openFilePicker();
        }}
      >
        <div className="dz-ico">
          <Icon name="upload" />
        </div>
        <h2>Drop CSV files here</h2>
        <p>or browse your disk — multiple files supported, merged automatically when columns match</p>
        <div className="dz-actions">
          <Button variant="primary" icon="folder" onClick={openFilePicker}>
            Browse files
          </Button>
          <Button
            onClick={loadDemo}
            disabled={demo.isPending}
            title="Creates a demo account with 14 months of fictional transactions to preview the charts"
          >
            {demo.isPending ? 'Loading demo data…' : 'Load demo data'}
          </Button>
          <Button
            icon="plus"
            onClick={() => {
              useCashDialogs.getState().openNewAccount();
            }}
            title="A wallet for cash: you add its expenses by hand"
          >
            New cash account
          </Button>
        </div>
        <div className="tips">
          <div className="tip">
            <b>Search</b>Every column, accent-insensitive. <kbd>{modKey('F')}</kbd>
          </div>
          <div className="tip">
            <b>Sort &amp; filter</b>Click a header to sort, use its funnel to filter.
          </div>
          <div className="tip">
            <b>Conditions</b>
            <code>&gt;100</code> <code>!uber</code> <code>2025-01..2025-03</code>
          </div>
        </div>
      </div>
    </div>
  );
}
