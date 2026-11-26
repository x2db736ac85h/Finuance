import { beforeEach, describe, expect, it } from 'vitest';

import { dragKind, dropKind, MAX_CHAT_FILE_BYTES, useChatDrops } from './drops';

describe('files dropped for the assistant (Phase 27)', () => {
  beforeEach(() => {
    useChatDrops.setState({ files: [] });
  });

  it('tells statements, pictures and PDFs apart by type or by name', () => {
    expect(dropKind({ name: 'wyciag.csv', type: 'text/csv' })).toBe('import');
    expect(dropKind({ name: 'export.TXT', type: '' })).toBe('import');
    expect(dropKind({ name: 'paragon.jpg', type: 'image/jpeg' })).toBe('photo');
    expect(dropKind({ name: 'IMG_0001.HEIC', type: '' })).toBe('photo');
    expect(dropKind({ name: 'faktura.pdf', type: 'application/pdf' })).toBe('pdf');
    expect(dropKind({ name: 'faktura.PDF', type: '' })).toBe('pdf');
    expect(dropKind({ name: 'budget.xlsx', type: 'application/vnd.ms-excel' })).toBe('other');
    expect(MAX_CHAT_FILE_BYTES).toBe(10 * 1024 * 1024);
  });

  it('reads a drag still in the air from the kinds of its files', () => {
    expect(dragKind(['image/png', 'application/pdf'])).toBe('chat');
    expect(dragKind(['text/csv'])).toBe('import');
    expect(dragKind(['text/csv', 'image/png'])).toBe('mixed');
    // a browser that doesn't say: nothing is promised
    expect(dragKind([''])).toBe('mixed');
    expect(dragKind([])).toBe('mixed');
  });

  it('hands waiting files over once', () => {
    const a = new File(['a'], 'a.png', { type: 'image/png' });
    const b = new File(['b'], 'b.pdf', { type: 'application/pdf' });
    useChatDrops.getState().give([a]);
    useChatDrops.getState().give([b]);
    expect(useChatDrops.getState().take()).toEqual([a, b]);
    expect(useChatDrops.getState().take()).toEqual([]);
  });
});
