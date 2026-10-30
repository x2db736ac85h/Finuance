import { act, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';

import type { ImportChanges, ImportDetail, ImportPreview } from '../api/hooks/imports';
import { IMPORT_ID, preview, stagedRow } from '../test/importFixtures';
import { renderApp, warmUpApp } from '../test/render';
import { account, API, db, problem, resetDb, server } from '../test/server';
import { importFiles, resetQueue } from './queue';

/** An in-memory import endpoint set around one preview. */
let cur: ImportPreview;
let status: ImportDetail['status'];
let patches: ImportChanges[];
let commits: number;
let deleted: string[];
let failNextCommit: boolean;
/** The server's fast path (autoCommit=true commits a known layout). */
let fastPath: boolean;
let uploads: number;

function detail(): ImportDetail {
  return {
    ...cur.import,
    status,
    stats: status === 'committed' ? { rows: cur.stats.rows, new: 2, duplicates: 1 } : { rows: cur.stats.rows },
    ...(status === 'failed' ? { error: 'The database went away. Try again.' } : {}),
  };
}

const lines = {
  encoding: 'utf-8',
  delimiter: ';',
  total: 4,
  lines: [
    { record: 1, fields: ['Data', 'Opis', 'Kwota'], status: 'header' },
    { record: 2, fields: ['02.01.2025', 'Sklep 0', '-10,00'], status: 'data' },
    { record: 3, fields: ['03.01.2025', 'Sklep 1', '-11,00'], status: 'data' },
    { record: 4, fields: ['Saldo', '123,45'], status: 'footer_dropped' },
  ],
};

beforeAll(async () => {
  server.listen({ onUnhandledFrame: 'error' });
  await warmUpApp();
});
beforeEach(() => {
  resetDb();
  resetQueue();
  cur = preview();
  status = 'previewed';
  patches = [];
  commits = 0;
  deleted = [];
  failNextCommit = false;
  fastPath = false;
  uploads = 0;
  server.use(
    http.post(`${API}/imports`, ({ request }) => {
      // every upload is a new import: IMPORT_ID first, then …f2, …f3
      cur = {
        ...cur,
        import: { ...cur.import, id: uploads++ ? `${IMPORT_ID.slice(0, -1)}${String(uploads + 1)}` : IMPORT_ID },
      };
      const auto = new URL(request.url).searchParams.get('autoCommit') === 'true';
      if (auto && fastPath && db.accounts.length) {
        status = 'committing';
        cur = { ...cur, import: { ...cur.import, accountId: db.accounts[0]?.id ?? '' } };
        setTimeout(() => {
          status = 'committed';
        }, 30);
        return HttpResponse.json(cur, { status: 202 });
      }
      return HttpResponse.json(cur, { status: 201 });
    }),
    http.get(`${API}/imports/:id`, () => HttpResponse.json(detail())),
    http.patch(`${API}/imports/:id`, async ({ request }) => {
      const ch = (await request.json()) as ImportChanges;
      patches.push(ch);
      if (ch.mapping?.bookedOn?.index === 9) {
        return problem(422, 'validation_failed', 'Invalid mapping.', {
          errors: [{ field: 'mapping.bookedOn', message: 'Column 10 does not exist in this file.' }],
        });
      }
      cur = {
        ...cur,
        import: {
          ...cur.import,
          ...(ch.accountId ? { accountId: ch.accountId } : {}),
          ...(ch.mapping ? { mapping: ch.mapping } : {}),
          ...(ch.options ? { options: ch.options } : {}),
        },
      };
      return HttpResponse.json(cur);
    }),
    http.get(`${API}/imports/:id/lines`, () => HttpResponse.json(lines)),
    http.get(`${API}/imports/:id/rows`, ({ request }) => {
      const only = new URL(request.url).searchParams.get('onlyErrors') === 'true';
      const rows = only
        ? [stagedRow(7, { error: 'No date in this row' }), stagedRow(8, { error: 'Amount is not a number' })]
        : cur.sampleRows;
      return HttpResponse.json({ rows, total: rows.length });
    }),
    http.post(`${API}/imports/:id/commit`, () => {
      commits++;
      status = 'committing';
      const fail = failNextCommit;
      failNextCommit = false;
      setTimeout(() => {
        status = fail ? 'failed' : 'committed';
      }, 30);
      return HttpResponse.json(detail(), { status: 202 });
    }),
    http.delete(`${API}/imports/:id`, ({ params }) => {
      deleted.push(String(params['id']));
      return new HttpResponse(null, { status: 204 });
    }),
    http.post(`${API}/accounts`, async ({ request }) => {
      const { name } = (await request.json()) as { name: string };
      const a = account({ id: '00000000-0000-4000-8000-0000000000a9', name });
      db.accounts = [...db.accounts, a];
      return HttpResponse.json(a, { status: 201 });
    }),
  );
});
afterEach(() => {
  server.resetHandlers();
});
afterAll(() => {
  server.close();
});

const csv = (name = 'wyciag.csv') =>
  new File(['Data;Opis;Kwota\n02.01.2025;Sklep;-10,00\n'], name, { type: 'text/csv' });

async function openUpload() {
  const user = userEvent.setup();
  const view = renderApp('/');
  await screen.findByRole('heading', { name: 'Drop CSV files here' });
  act(() => {
    importFiles([csv()]);
  });
  const dlg = await screen.findByRole('dialog', { name: 'Import' });
  await within(dlg).findByText('Sklep 0');
  return { user, dlg, ...view };
}

describe('import window', () => {
  it('previews an upload, creates the account and imports', async () => {
    const { user, dlg, router } = await openUpload();
    expect(router.state.location.pathname).toBe(`/imports/${IMPORT_ID}`);
    expect(within(dlg).getByText(/wyciag\.csv/)).toBeTruthy();
    const importBtn = within(dlg).getByRole('button', { name: 'Import 3 transactions' });
    expect((importBtn as HTMLButtonElement).disabled).toBe(true); // no account yet

    // the suggested name is offered for a new account
    const name = within(dlg).getByRole('textbox', { name: 'New account name' });
    expect((name as HTMLInputElement).value).toBe('Konto …0001');
    await user.click(within(dlg).getByRole('button', { name: 'Create' }));
    await waitFor(() => {
      expect(patches.at(-1)?.accountId).toBe('00000000-0000-4000-8000-0000000000a9');
    });
    await waitFor(() => {
      expect((importBtn as HTMLButtonElement).disabled).toBe(false);
    });

    await user.click(importBtn);
    expect(await screen.findByText('Imported 2 transactions · 1 duplicate skipped')).toBeTruthy();
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/t/00000000-0000-4000-8000-0000000000a9');
    });
    expect(commits).toBe(1);
  });

  it('sets a column meaning from the role menu', async () => {
    const { user, dlg } = await openUpload();
    await user.click(within(dlg).getByRole('button', { name: /Opis/ }));
    const menu = await screen.findByRole('menu', { name: 'Meaning of Opis' });
    expect(within(menu).getByRole('menuitem', { name: /Name — payee \/ merchant/ }).textContent).toContain('auto');
    await user.click(within(menu).getByRole('menuitem', { name: 'Description / title' }));
    await waitFor(() => {
      expect(patches.at(-1)?.mapping).toMatchObject({ description: { index: 1, header: 'Opis' } });
    });
    expect(patches.at(-1)?.mapping?.payee).toBeUndefined();
    expect(within(dlg).getByRole('button', { name: /Opis/ }).textContent).toContain('→ Description');
    // the required-fields footer follows
    expect(within(dlg).getByText(/Name$/).textContent).toContain('!');
  });

  it('Use detected meanings restores what detection found', async () => {
    const { user, dlg } = await openUpload();
    await user.click(within(dlg).getByRole('button', { name: /Opis/ }));
    await user.click(await screen.findByRole('menuitem', { name: /Not mapped/ }));
    await waitFor(() => {
      expect(patches.at(-1)?.mapping?.payee).toBeUndefined();
    });
    await user.click(await within(dlg).findByRole('button', { name: 'Use detected meanings' }));
    await waitFor(() => {
      expect(patches.at(-1)?.mapping?.payee).toEqual({ index: 1, header: 'Opis' });
    });
  });

  it('picks the header row from the raw lines and switches to no header', async () => {
    const { user, dlg } = await openUpload();
    const raw = within(dlg).getByRole('listbox', { name: 'Raw file lines' });
    await within(raw).findAllByRole('option');
    expect(within(raw).getAllByRole('option')[0]?.className).toBe('rl hdr');
    expect(within(raw).getAllByRole('option')[3]?.textContent).toContain('summary');
    await user.click(within(raw).getAllByRole('option')[1] as HTMLElement);
    await waitFor(() => {
      expect(patches.at(-1)?.options?.headerRow).toBe(2);
    });
    await user.click(within(dlg).getByRole('button', { name: 'File has no header row' }));
    await waitFor(() => {
      expect(patches.at(-1)?.options).toMatchObject({ headerRow: 'none', skipRows: 1 });
    });
    expect(await within(dlg).findByLabelText('Data starts at row')).toBeTruthy();
  });

  it('debounces option edits into one PATCH with the full options', async () => {
    const { user, dlg } = await openUpload();
    await user.click(within(dlg).getByRole('checkbox', { name: /Invert amount sign/ }));
    await user.selectOptions(within(dlg).getByRole('combobox', { name: 'Dates' }), 'mdy');
    await waitFor(() => {
      expect(patches).toHaveLength(1);
    });
    expect(patches[0]?.options).toMatchObject({
      invertSign: true,
      dateOrder: 'mdy',
      encoding: 'auto',
      amountMode: 'single',
    });
  });

  it('shows server validation errors and keeps the saved mapping', async () => {
    const headers = ['Data', 'Opis', 'Kwota', 'd', 'e', 'f', 'g', 'h', 'i', 'j'];
    cur = preview({ import: { ...preview().import, headers, columnTypes: headers.map(() => 'text' as const) } });
    const { user, dlg } = await openUpload();
    await user.click(within(dlg).getByRole('button', { name: /Column meanings/ }));
    await user.selectOptions(within(dlg).getByRole('combobox', { name: 'Date' }), '9'); // the fake server rejects it
    expect(await within(dlg).findAllByText('Column 10 does not exist in this file.')).toHaveLength(2); // banner + row
    await waitFor(() => {
      expect(within(dlg).getByRole<HTMLSelectElement>('combobox', { name: 'Date' }).value).toBe('0');
    });
  });

  it('lists the rows with errors', async () => {
    cur = preview({ stats: { ...preview().stats, errors: 2 } });
    const { user, dlg } = await openUpload();
    await user.click(within(dlg).getByRole('button', { name: '2 rows with errors' }));
    expect(await within(dlg).findByText('No date in this row')).toBeTruthy();
    expect(within(dlg).getByText('Amount is not a number')).toBeTruthy();
    await user.click(within(dlg).getByRole('button', { name: 'Show all rows' }));
    expect(await within(dlg).findByText('Sklep 0')).toBeTruthy();
  });

  it('cancel deletes the preview', async () => {
    const { user, dlg, router } = await openUpload();
    await user.click(within(dlg).getByRole('button', { name: 'Cancel' }));
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/t/all');
    });
    expect(deleted).toEqual([IMPORT_ID]);
  });

  it('a failed commit offers Try again', async () => {
    db.accounts = [account()];
    cur = { ...cur, import: { ...cur.import, accountId: db.accounts[0]?.id ?? '' } };
    failNextCommit = true;
    const { user, dlg, router } = await openUpload();
    await user.click(within(dlg).getByRole('button', { name: 'Import 3 transactions' }));
    const err = await screen.findByText(/The database went away/);
    await user.click(within(err).getByRole('button', { name: 'Try again' }));
    expect(await screen.findByText('Imported 2 transactions · 1 duplicate skipped')).toBeTruthy();
    await waitFor(() => {
      expect(router.state.location.pathname).toBe(`/t/${db.accounts[0]?.id ?? ''}`);
    });
    expect(commits).toBe(2);
  });
});

describe('upload queue', () => {
  it('reports a file imported before, with a link', async () => {
    server.use(
      http.post(`${API}/imports`, () =>
        problem(409, 'duplicate_file', 'Already imported.', { existingImportId: IMPORT_ID }),
      ),
    );
    const user = userEvent.setup();
    const { router } = renderApp('/');
    await screen.findByRole('heading', { name: 'Drop CSV files here' });
    act(() => {
      importFiles([csv('again.csv')]);
    });
    const msg = await screen.findByText(/again\.csv was already imported on/);
    await user.click(within(msg).getByRole('button', { name: 'Open' }));
    await waitFor(() => {
      expect(router.state.location.pathname).toBe(`/imports/${IMPORT_ID}`);
    });
  });

  it('fast path: a known layout is imported at once, with Undo', async () => {
    db.accounts = [account({ txCount: 3 })];
    fastPath = true;
    const user = userEvent.setup();
    renderApp('/');
    await waitFor(() => {
      expect(document.querySelector('.toolbar')).not.toBeNull();
    });
    act(() => {
      importFiles([csv()]);
    });
    const msg = await screen.findByText(/Imported 2 transactions · 1 duplicate skipped from wyciag\.csv/);
    expect(screen.queryByRole('dialog')).toBeNull();
    await user.click(within(msg).getByRole('button', { name: 'Undo' }));
    await waitFor(() => {
      expect(deleted).toEqual([IMPORT_ID]);
    });
    expect(await screen.findByText('Removed the import of wyciag.csv')).toBeTruthy();
  });

  it('refuses files that are not CSV', async () => {
    renderApp('/');
    await screen.findByRole('heading', { name: 'Drop CSV files here' });
    act(() => {
      importFiles([new File(['x'], 'photo.png', { type: 'image/png' })]);
    });
    expect(await screen.findByText(/Skipped 1 file that isn't CSV, TSV or TXT/)).toBeTruthy();
  });

  it('shows the position in a batch and moves to the next file', async () => {
    const user = userEvent.setup();
    renderApp('/');
    await screen.findByRole('heading', { name: 'Drop CSV files here' });
    act(() => {
      importFiles([csv('a.csv'), csv('b.csv')]);
    });
    const dlg = await screen.findByRole('dialog', { name: 'Import' });
    expect(await within(dlg).findByText(/file 1 of 2/)).toBeTruthy();
    await user.click(await within(dlg).findByRole('button', { name: 'Cancel' }));
    expect(await screen.findByText(/file 2 of 2/)).toBeTruthy();
  });
});

describe('upload target', () => {
  it('uploads into the account in view', async () => {
    db.accounts = [account({ txCount: 3 })];
    let sent: string | null = null;
    server.use(
      http.post(`${API}/imports`, async ({ request }) => {
        sent = (await request.formData()).get('accountId') as string | null;
        return HttpResponse.json(cur, { status: 201 });
      }),
    );
    renderApp(`/t/${db.accounts[0]?.id ?? ''}`);
    await waitFor(() => {
      expect(document.querySelector('.toolbar')).not.toBeNull();
    });
    act(() => {
      importFiles([csv()]);
    });
    await screen.findByRole('dialog', { name: 'Import' });
    expect(sent).toBe(db.accounts[0]?.id);
  });
});
