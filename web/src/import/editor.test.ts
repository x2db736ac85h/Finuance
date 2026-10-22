import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ApiError } from '../api/client';
import type { ImportChanges, ImportPreview } from '../api/hooks/imports';
import { preview } from '../test/importFixtures';
import { effective, PreviewEditor } from './editor';

beforeEach(() => {
  vi.useFakeTimers();
});
afterEach(() => {
  vi.useRealTimers();
});

/** A fake PATCH that applies the changes to a preview. */
function server() {
  let cur = preview();
  const calls: ImportChanges[] = [];
  const patch = vi.fn((ch: ImportChanges): Promise<ImportPreview> => {
    calls.push(ch);
    cur = {
      ...cur,
      import: {
        ...cur.import,
        ...(ch.accountId ? { accountId: ch.accountId } : {}),
        ...(ch.mapping ? { mapping: ch.mapping } : {}),
        ...(ch.options ? { options: ch.options } : {}),
      },
    };
    return Promise.resolve(cur);
  });
  return { patch, calls };
}

describe('PreviewEditor', () => {
  it('loads with an empty PATCH', async () => {
    const s = server();
    const ed = new PreviewEditor(s.patch);
    await ed.load();
    expect(s.calls).toEqual([{}]);
    expect(ed.getState().preview?.import.id).toBeDefined();
  });

  it('batches changes for 300 ms and sends the full options document', async () => {
    const s = server();
    const ed = new PreviewEditor(s.patch);
    ed.set(preview());
    ed.change({ options: { invertSign: true } });
    ed.change({ options: { dateOrder: 'mdy' } });
    expect(effective(ed.getState())?.options).toMatchObject({ invertSign: true, dateOrder: 'mdy', encoding: 'auto' });
    await vi.advanceTimersByTimeAsync(299);
    expect(s.patch).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1);
    expect(s.calls).toHaveLength(1);
    expect(s.calls[0]?.options).toMatchObject({
      invertSign: true,
      dateOrder: 'mdy',
      headerRow: 'auto',
      amountMode: 'single',
    });
    expect(s.calls[0]?.mapping).toBeUndefined();
    expect(ed.getState().draft).toBeNull();
    expect(ed.getState().preview?.import.options.invertSign).toBe(true);
  });

  it('keeps changes made during a request for the next one', async () => {
    let release: (p: ImportPreview) => void = () => undefined;
    const patch = vi.fn(
      (ch: ImportChanges) =>
        new Promise<ImportPreview>((res) => {
          release = (p) => {
            res({ ...p, import: { ...p.import, ...(ch.options ? { options: ch.options } : {}) } });
          };
        }),
    );
    const ed = new PreviewEditor(patch);
    ed.set(preview());
    ed.change({ options: { invertSign: true } });
    await vi.advanceTimersByTimeAsync(300);
    expect(patch).toHaveBeenCalledTimes(1);
    ed.change({ accountId: 'acc-1' });
    release(preview());
    await vi.advanceTimersByTimeAsync(0);
    expect(ed.getState().draft?.accountId).toBe('acc-1');
    await vi.advanceTimersByTimeAsync(300);
    expect(patch).toHaveBeenCalledTimes(2);
    expect(patch.mock.calls[1]?.[0].accountId).toBe('acc-1');
  });

  it('drops the pending change on 422 and keeps the problem', async () => {
    const problem = {
      type: '/problems/validation_failed',
      title: 'Invalid',
      status: 422,
      code: 'validation_failed' as const,
      errors: [{ field: 'mapping.bookedOn', message: 'column 9 does not exist' }],
    };
    const patch = vi.fn(() => Promise.reject(new ApiError(problem)));
    const ed = new PreviewEditor(patch);
    ed.set(preview());
    ed.change({ mapping: { ignore: [], bookedOn: { index: 9, header: 'x' } } });
    await ed.flush();
    expect(ed.getState().draft).toBeNull();
    expect(ed.getState().problem?.errors?.[0]?.field).toBe('mapping.bookedOn');
    expect(effective(ed.getState())?.mapping.bookedOn?.index).toBe(0);
  });

  it('flush sends at once and notifies listeners', async () => {
    const s = server();
    const ed = new PreviewEditor(s.patch);
    const seen = vi.fn();
    ed.subscribe(seen);
    ed.set(preview());
    ed.change({ accountId: 'acc-2' });
    await ed.flush();
    expect(s.calls).toEqual([{ accountId: 'acc-2' }]);
    expect(seen.mock.calls.length).toBeGreaterThanOrEqual(3);
    await ed.flush(); // nothing pending
    expect(s.calls).toHaveLength(1);
  });
});
