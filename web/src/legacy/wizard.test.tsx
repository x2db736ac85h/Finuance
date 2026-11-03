import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';

import locked from '../../../testdata/fixtures/legacy-backup/backup-locked.json?raw';
import plain from '../../../testdata/fixtures/legacy-backup/backup-plain.json?raw';
import type { LegacyImport } from '../api/hooks/legacy';
import { useUi } from '../stores/uiStore';
import { renderApp, warmUpApp } from '../test/render';
import { API, db, resetDb, server } from '../test/server';
import { openBackup, parseBackup, fileInfo } from './backup';
import { defaultPlan } from './LegacyWizard';

beforeAll(async () => {
  server.listen({ onUnhandledFrame: 'error' });
  await warmUpApp();
});
beforeEach(() => {
  resetDb();
});
afterEach(() => {
  server.resetHandlers();
});
afterAll(() => {
  server.close();
});

const report: NonNullable<LegacyImport['report']> = {
  files: [
    {
      legacyId: 'a',
      name: 'demo.csv',
      status: 'imported',
      accountName: 'Account 1',
      rows: 917,
      new: 917,
      duplicates: 0,
      errors: 0,
    },
    {
      legacyId: 'b',
      name: 'v1_jan-feb.csv',
      status: 'failed',
      accountName: 'Account 2',
      rows: 0,
      new: 0,
      duplicates: 0,
      errors: 0,
      message: 'no column is mapped to amount',
    },
  ],
  accounts: [{ id: '00000000-0000-0000-0000-00000000000a', name: 'Account 1', created: true }],
  transactions: { new: 917, duplicates: 4, errors: 0 },
  groups: { created: 2, reused: 0 },
  labels: {
    matched: 7,
    unmatched: 1,
    examples: [{ what: 'Remont', key: '2025-03-02 | -12.00 | kawa', reason: 'its row was not imported' }],
  },
  overrides: { matched: 2, unmatched: 0, examples: [] },
  budgets: { set: 3, skipped: [] },
  opening: { accountId: '00000000-0000-0000-0000-00000000000a', accountName: 'Account 1', amountMinor: 150000 },
  investments: 'imported',
  chats: { imported: 2, skipped: 0 },
  unknownKeys: [],
};

/** The legacy endpoints: one run that is "running" on its first poll and "done" afterwards. */
function legacyServer() {
  const sent: { manifest: Record<string, unknown> | null; files: number } = { manifest: null, files: 0 };
  let polls = 0;
  const base = { id: 'r1', createdAt: '2026-10-23T10:00:00Z' };
  server.use(
    http.get(`${API}/assistant/settings`, () => HttpResponse.json({ retentionDays: 2, cloudConsent: [] })),
    http.get(`${API}/legacy-imports`, () => HttpResponse.json([])),
    http.post(`${API}/legacy-imports`, async ({ request }) => {
      const fd = await request.formData();
      const m = fd.get('manifest');
      sent.manifest = typeof m === 'string' ? (JSON.parse(m) as Record<string, unknown>) : null;
      sent.files = fd.getAll('file').length;
      return HttpResponse.json(
        { ...base, status: 'queued', progress: { filesDone: 0, filesTotal: 3, step: 'files' } },
        { status: 202 },
      );
    }),
    http.get(`${API}/legacy-imports/r1`, () => {
      polls++;
      return polls === 1
        ? HttpResponse.json({
            ...base,
            status: 'running',
            progress: { filesDone: 1, filesTotal: 3, step: 'files', current: 'v2_jan-mar.csv' },
          })
        : HttpResponse.json({
            ...base,
            status: 'done',
            progress: { filesDone: 3, filesTotal: 3, step: 'done' },
            report,
            finishedAt: '2026-10-23T10:00:05Z',
          });
    }),
  );
  return sent;
}

async function chooseBackup(
  user: ReturnType<typeof userEvent.setup>,
  text: string,
  name = 'finuance-backup-2025-03-20.json',
) {
  renderApp('/settings/data');
  const input = await screen.findByLabelText('Backup file');
  await user.upload(input, new File([text], name, { type: 'application/json' }));
  return screen.findByRole('dialog', { name: 'Import from the single-file app' });
}

describe('legacy migration wizard', () => {
  it('reviews a plain backup, sends the chosen accounts and shows the report', async () => {
    const sent = legacyServer();
    const user = userEvent.setup();
    const dialog = await chooseBackup(user, plain);
    await within(dialog).findByText(
      /3 files · 2 groups · 8 labels · 3 budgets · 2 duplicate decisions · investments · 2 chats/,
    );
    // the three files share one layout and no account number of an existing account: one new account
    const names = within(dialog).getAllByRole('textbox', { name: /Name of new account/ });
    expect(names).toHaveLength(1);
    await user.clear(names[0] as HTMLElement);
    await user.type(names[0] as HTMLElement, 'Demo');
    expect(within(dialog).getByText(/Opening balance of/)).toBeTruthy();
    expect(within(dialog).getByText(/kept for 2 days after their last message/)).toBeTruthy();
    await user.click(within(dialog).getByRole('checkbox', { name: /Include the 2 assistant chats/ }));
    await user.click(within(dialog).getByRole('button', { name: 'Start migration' }));

    await within(dialog).findByText(/Importing v2_jan-mar.csv \(2 of 3\)/);
    await within(dialog).findByText(/917 new transactions, 4 duplicates skipped/, {}, { timeout: 3000 });
    expect(within(dialog).getByText('Not imported')).toBeTruthy();
    expect(within(dialog).getByText(/Labels: 7 of 8 found their transactions/)).toBeTruthy();
    expect(within(dialog).getByText(/1 label without a transaction/)).toBeTruthy();

    const m = sent.manifest as {
      files: { name: string; account: unknown }[];
      chats?: unknown;
      private: Record<string, unknown>;
    };
    expect(sent.files).toBe(3);
    expect(m.files.map((f) => [f.name, f.account])).toEqual([
      ['demo.csv', { new: { name: 'Demo' } }],
      ['v2_jan-mar.csv', { new: { name: 'Demo' } }],
      ['v1_jan-feb.csv', { new: { name: 'Demo' } }],
    ]);
    expect(m.chats).toBeUndefined(); // unticked
    expect(m.private['aiKey']).toBeUndefined();
    expect(m.private['tags']).toBeTruthy();
    // the old app's appearance settings were applied
    expect(useUi.getState().catMode).toBe('donut');
  }, 15000);

  it('asks for the passphrase of a locked backup and refuses a wrong one', async () => {
    legacyServer();
    const user = userEvent.setup();
    const dialog = await chooseBackup(user, locked);
    const pass = await within(dialog).findByLabelText('Passphrase');
    await user.type(pass, 'wrong');
    await user.click(within(dialog).getByRole('button', { name: 'Unlock' }));
    await within(dialog).findByText(/The passphrase is wrong/, {}, { timeout: 5000 });
    await user.clear(pass);
    await user.type(pass, 'oracle-test-passphrase');
    await user.click(within(dialog).getByRole('button', { name: 'Unlock' }));
    await within(dialog).findByText(/3 files · 2 groups/, {}, { timeout: 5000 });
  }, 20000);

  it('says so for a file that is not a backup', async () => {
    legacyServer();
    const user = userEvent.setup();
    const dialog = await chooseBackup(user, '{"hello":1}', 'notes.json');
    await within(dialog).findByText('This is not a backup of the single-file app.');
  });

  it('shows the last migration and its report', async () => {
    server.use(
      http.get(`${API}/legacy-imports`, () =>
        HttpResponse.json([
          {
            id: 'r1',
            status: 'done',
            progress: { filesDone: 2, filesTotal: 2, step: 'done' },
            report,
            createdAt: '2026-10-23T10:00:00Z',
          },
        ]),
      ),
      http.get(`${API}/legacy-imports/r1`, () =>
        HttpResponse.json({
          id: 'r1',
          status: 'done',
          progress: { filesDone: 2, filesTotal: 2, step: 'done' },
          report,
          createdAt: '2026-10-23T10:00:00Z',
        }),
      ),
    );
    const user = userEvent.setup();
    renderApp('/settings/data');
    await screen.findByText(/Last migration .*: finished/);
    await user.click(screen.getByRole('button', { name: 'View report' }));
    const dialog = await screen.findByRole('dialog', { name: 'Migration from the single-file app' });
    await within(dialog).findByText(/Budgets: 3 set/);
  });
});

describe('defaultPlan', () => {
  it('puts a file whose account number matches an account there, and one new account per layout', async () => {
    const o = await openBackup(parseBackup(plain));
    const infos = Object.fromEntries(o.files.map((f) => [f.id, fileInfo(f)]));
    const plan = defaultPlan(o, infos, []);
    expect(plan.names).toEqual(['Account 1']);
    expect(o.files.map((f) => plan.picks[f.id])).toEqual(['new:0', 'new:0', 'new:0']);
    const other = {
      ...o,
      files: [
        ...o.files,
        { id: 'x', name: 'other.csv', size: 3, added: 9, cfg: null, data: new TextEncoder().encode('A;B\n1;2\n') },
      ],
    };
    const two = defaultPlan(other, { ...infos, x: fileInfo(other.files[3] as (typeof other.files)[number]) }, []);
    expect(two.names).toEqual(['Account 1', 'Account 2']);
    expect(two.picks['x']).toBe('new:1');

    // the duplicate-version files carry the account number ending in 0001
    const v2File = o.files[1];
    if (!v2File) throw new Error('no file');
    expect(infos[v2File.id]?.accountHints).toContain('0001');
    const acc = {
      ...db.accounts[0],
      id: 'acc',
      name: 'Konto',
      numberHint: '0001',
      currency: 'PLN',
    } as (typeof db.accounts)[number];
    const withAccount = defaultPlan(o, infos, [acc]);
    expect(withAccount.names).toEqual(['Account 1']);
    expect(o.files.map((f) => withAccount.picks[f.id])).toEqual(['new:0', 'id:acc', 'id:acc']);
  });
});
