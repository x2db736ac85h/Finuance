import { describe, expect, it } from 'vitest';

import locked from '../../../testdata/fixtures/legacy-backup/backup-locked.json?raw';
import plain from '../../../testdata/fixtures/legacy-backup/backup-plain.json?raw';
import {
  BackupError,
  buildManifest,
  deser,
  fileInfo,
  isLocked,
  legacyPrefs,
  openBackup,
  parseBackup,
  summary,
  WrongPassphrase,
} from './backup';

describe('legacy backups', () => {
  it('deser turns {$b} into bytes and keeps the rest', () => {
    const v = deser({ a: { $b: btoa('hi') }, b: [1, { $b: btoa('x'), $u8: 1 }], c: 'z' }) as {
      a: Uint8Array;
      b: [number, Uint8Array];
      c: string;
    };
    expect(new TextDecoder().decode(v.a)).toBe('hi');
    expect(v.b[1]).toBeInstanceOf(Uint8Array);
    expect(v.c).toBe('z');
  });

  it('refuses files that are not backups', () => {
    expect(() => parseBackup('not json')).toThrow(BackupError);
    expect(() => parseBackup('{"other":1}')).toThrow(/not a backup/);
    expect(parseBackup('{"csvInspectorBackup":1,"kv":{},"files":[]}').source).toBe('csvInspectorBackup');
  });

  it('opens the plain backup', async () => {
    const b = parseBackup(plain);
    expect(isLocked(b)).toBe(false);
    const o = await openBackup(b);
    expect(o.files.map((f) => f.name)).toEqual(['demo.csv', 'v2_jan-mar.csv', 'v1_jan-feb.csv']);
    expect(summary(o)).toEqual({
      files: 3,
      groups: 2,
      labels: 8,
      budgets: 3,
      decisions: 2,
      chats: 2,
      investments: true,
      opening: 1500,
    });
    const demo = o.files[0];
    if (!demo) throw new Error('no demo file');
    const info = fileInfo(demo);
    expect(info.from).toBe('2024-01-01');
    expect(info.to).toBe('2025-03-17');
    expect(info.layout).toMatch(/^Data księgowania;/);
  });

  it('opens the locked backup with its passphrase only', async () => {
    const b = parseBackup(locked);
    expect(isLocked(b)).toBe(true);
    await expect(openBackup(b, 'wrong')).rejects.toBeInstanceOf(WrongPassphrase);
    const o = await openBackup(b, 'oracle-test-passphrase');
    const p = await openBackup(parseBackup(plain));
    // the same content as the plain backup once decrypted
    expect(o.files.map((f) => [f.name, f.size])).toEqual(p.files.map((f) => [f.name, f.size]));
    expect(o.private['tags']).toEqual(p.private['tags']);
    expect(o.chats).toEqual(p.chats);
  }, 20000);

  it('builds the manifest without the AI key and with the chosen accounts', async () => {
    const o = await openBackup(parseBackup(plain));
    o.private['aiKey'] = 'sk-secret';
    const ids = o.files.map((f) => f.id);
    const [first = '', second = '', third = ''] = ids;
    const { manifest, files } = buildManifest(o, {
      targets: { [first]: { new: 'Demo ' }, [second]: { id: 'acc-1' }, [third]: { id: 'acc-1' } },
      opening: { id: 'acc-1' },
      includeChats: false,
    });
    expect(JSON.stringify(manifest)).not.toContain('sk-secret');
    expect(manifest['chats']).toBeUndefined();
    expect(manifest['opening']).toEqual({ id: 'acc-1' });
    const mf = manifest['files'] as { account: unknown; legacyId: string }[];
    expect(mf.map((f) => f.account)).toEqual([{ new: { name: 'Demo' } }, { id: 'acc-1' }, { id: 'acc-1' }]);
    expect(files).toHaveLength(3);
    expect(files[0]?.size).toBe(o.files[0]?.size);
    // a file without a target is left out
    expect(
      buildManifest(o, { targets: { [first]: { new: 'Demo' } }, opening: null, includeChats: true }).files,
    ).toHaveLength(1);
  });

  it('keeps the appearance settings, never the AI address or key', async () => {
    const o = await openBackup(parseBackup(plain));
    const p = legacyPrefs({ ...o.prefs, ai: { base: 'http://x', model: 'm', key: 'k', think: true, maxRows: 25 } });
    expect(p['theme']).toBe('system');
    expect(p['ai']).toEqual({ think: true, maxRows: 25 });
    expect(p['views']).toBeUndefined();
  });
});
