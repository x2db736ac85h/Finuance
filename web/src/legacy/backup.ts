// Reading a backup of the single-file legacy app (PLAN §4.10, task 14.1). Everything here runs in
// the browser: the passphrase and the encrypted data never leave it. The result is what the
// wizard shows and, after the user's choices, the manifest POST /legacy-imports takes.

/** Thrown for a file that is not a legacy backup, or one this reader can't make sense of. */
export class BackupError extends Error {}

/** The passphrase doesn't open the backup. */
export class WrongPassphrase extends Error {
  constructor() {
    super('The passphrase is wrong.');
  }
}

type Obj = Record<string, unknown>;
const isObj = (x: unknown): x is Obj => typeof x === 'object' && x !== null && !Array.isArray(x);

/** A legacy encrypted value (CRY.enc): AES-GCM, 12-byte iv, the tag at the end of ct. */
type Sealed = { iv: Uint8Array; ct: Uint8Array };
const isSealed = (x: unknown): x is Sealed =>
  isObj(x) && x['iv'] instanceof Uint8Array && x['ct'] instanceof Uint8Array;

function unb64(s: string): Uint8Array {
  const bin = atob(s);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

/** Port of the legacy deser: {$b} (and {$b, $u8}) become bytes, everything else is kept. */
export function deser(v: unknown): unknown {
  if (Array.isArray(v)) return v.map(deser);
  if (isObj(v)) {
    if (typeof v['$b'] === 'string') return unb64(v['$b']);
    const o: Obj = {};
    for (const [k, x] of Object.entries(v)) o[k] = deser(x);
    return o;
  }
  return v;
}

/** A parsed backup, possibly still locked. */
export type Backup = {
  source: 'finuanceBackup' | 'csvInspectorBackup';
  created: string;
  kv: Obj;
  files: Obj[];
  unknownKeys: string[];
};

const KNOWN_TOP = ['finuanceBackup', 'csvInspectorBackup', 'created', 'kv', 'files'];
const KNOWN_KV = ['prefs', 'private', 'lock', 'chats'];
const KNOWN_PRIVATE = ['budgets', 'opening', 'aiKey', 'groups', 'tags', 'dupKeep', 'dupPick', 'inv'];

/** Parses the text of a backup file (finuance-backup-*.json or an older csvInspectorBackup). */
export function parseBackup(text: string): Backup {
  let j: unknown;
  try {
    j = JSON.parse(text);
  } catch {
    throw new BackupError('This is not a backup file (it is not JSON).');
  }
  if (!isObj(j) || !(j['finuanceBackup'] || j['csvInspectorBackup'])) {
    throw new BackupError('This is not a backup of the single-file app.');
  }
  const kv = deser(j['kv']);
  const files = deser(j['files']);
  if (!isObj(kv) || !Array.isArray(files) || !files.every(isObj)) {
    throw new BackupError('The backup is damaged: it has no data or no files.');
  }
  const unknownKeys = [
    ...Object.keys(j).filter((k) => !KNOWN_TOP.includes(k)),
    ...Object.keys(kv)
      .filter((k) => !KNOWN_KV.includes(k))
      .map((k) => `kv.${k}`),
  ];
  return {
    source: j['finuanceBackup'] ? 'finuanceBackup' : 'csvInspectorBackup',
    created: typeof j['created'] === 'string' ? j['created'] : '',
    kv,
    files,
    unknownKeys,
  };
}

export const isLocked = (b: Backup) => isObj(b.kv['lock']);

/** One file of an opened backup. */
export type LegacyFile = {
  id: string;
  name: string;
  size: number;
  added: number;
  cfg: Obj | null;
  data: Uint8Array;
};

/** An opened (decrypted) backup. */
export type Opened = {
  source: Backup['source'];
  created: string;
  prefs: Obj;
  private: Obj;
  chats: unknown[] | null; // null: the backup has none
  files: LegacyFile[];
  unknownKeys: string[];
};

const PBKDF2_ITERATIONS = 310000;
const VERIFIERS = ['finuance', 'csv-inspector'];

async function deriveKey(pass: string, salt: Uint8Array): Promise<CryptoKey> {
  const base = await crypto.subtle.importKey('raw', new TextEncoder().encode(pass), 'PBKDF2', false, ['deriveKey']);
  return crypto.subtle.deriveKey(
    { name: 'PBKDF2', salt: salt as BufferSource, iterations: PBKDF2_ITERATIONS, hash: 'SHA-256' },
    base,
    { name: 'AES-GCM', length: 256 },
    false,
    ['decrypt'],
  );
}

async function decrypt(key: CryptoKey, s: Sealed): Promise<Uint8Array> {
  return new Uint8Array(
    await crypto.subtle.decrypt({ name: 'AES-GCM', iv: s.iv as BufferSource }, key, s.ct as BufferSource),
  );
}

async function decryptJSON(key: CryptoKey, s: Sealed): Promise<unknown> {
  return JSON.parse(new TextDecoder().decode(await decrypt(key, s))) as unknown;
}

/**
 * Opens a backup: with a lock, the passphrase is checked first (the legacy verifier decrypts to
 * "finuance" or, for locks made under the old name, "csv-inspector"); then the private data, the
 * chats and every file are decrypted. A file that fails to decrypt is left out, as the legacy did.
 */
export async function openBackup(b: Backup, passphrase = ''): Promise<Opened> {
  let key: CryptoKey | null = null;
  const lock = b.kv['lock'];
  if (isObj(lock)) {
    const salt = lock['salt'];
    const check = lock['check'];
    if (!(salt instanceof Uint8Array) || !isSealed(check)) throw new BackupError('The backup lock is damaged.');
    key = await deriveKey(passphrase, salt);
    const verifier = await decrypt(key, check).then(
      (b) => new TextDecoder().decode(b),
      () => null,
    );
    if (verifier === null || !VERIFIERS.includes(verifier)) throw new WrongPassphrase();
  }
  const plain = async (v: unknown): Promise<unknown> => {
    if (isObj(v) && isSealed(v['enc'])) {
      if (!key) throw new BackupError('The backup has encrypted data but no lock.');
      return decryptJSON(key, v['enc']);
    }
    return v;
  };
  const priv = await plain(b.kv['private']);
  const chats = await plain(b.kv['chats']);
  const files: LegacyFile[] = [];
  for (const f of b.files) {
    let data: Uint8Array | null = null;
    const d = f['data'];
    try {
      if (d instanceof Uint8Array) data = d;
      else if (isSealed(d) && key) data = await decrypt(key, d);
    } catch {
      data = null;
    }
    if (!data || typeof f['id'] !== 'string') continue;
    files.push({
      id: f['id'],
      name: typeof f['name'] === 'string' && f['name'] ? f['name'] : 'file.csv',
      size: data.byteLength,
      added: typeof f['added'] === 'number' ? f['added'] : 0,
      cfg: isObj(f['cfg']) ? f['cfg'] : null,
      data,
    });
  }
  files.sort((a, x) => a.added - x.added || (a.id < x.id ? -1 : a.id > x.id ? 1 : 0));
  const privObj = isObj(priv) ? priv : {};
  return {
    source: b.source,
    created: b.created,
    prefs: isObj(b.kv['prefs']) ? b.kv['prefs'] : {},
    private: privObj,
    chats: Array.isArray(chats) ? chats : null,
    files,
    unknownKeys: [
      ...b.unknownKeys,
      ...Object.keys(privObj)
        .filter((k) => !KNOWN_PRIVATE.includes(k))
        .map((k) => `kv.private.${k}`),
    ],
  };
}

/** Decodes a file's text as the import does: strict UTF-8, else Windows-1250. */
function text(data: Uint8Array): string {
  const head = data.subarray(0, 2 << 20);
  try {
    return new TextDecoder('utf-8', { fatal: true }).decode(head);
  } catch {
    return new TextDecoder('windows-1250').decode(head);
  }
}

const DATE_RE = /\b(\d{4})-(\d{2})-(\d{2})\b|\b(\d{2})\.(\d{2})\.(\d{4})\b/g;
const ACCOUNT_RE = /(?:PL)?\d{2}(?:[ ]?\d{4}){6}/g;

/** What the wizard shows about a file before anything is sent. */
export type FileInfo = {
  /** First and last date seen in the file (dd.mm.yyyy or yyyy-mm-dd), ISO. */
  from: string | null;
  to: string | null;
  /** Last four digits of each account number (26 digits) in the file. */
  accountHints: string[];
  /** The first line of the file: files with the same one share a layout (the legacy b1, b2…). */
  layout: string;
};

export function fileInfo(f: LegacyFile): FileInfo {
  const t = text(f.data);
  let from: string | null = null;
  let to: string | null = null;
  for (const m of t.matchAll(DATE_RE)) {
    const iso = m[1] ? `${m[1]}-${m[2] ?? ''}-${m[3] ?? ''}` : `${m[6] ?? ''}-${m[5] ?? ''}-${m[4] ?? ''}`;
    const month = Number(iso.slice(5, 7));
    const day = Number(iso.slice(8, 10));
    if (month < 1 || month > 12 || day < 1 || day > 31) continue;
    if (!from || iso < from) from = iso;
    if (!to || iso > to) to = iso;
  }
  const hints = new Set<string>();
  for (const m of t.matchAll(ACCOUNT_RE)) {
    const digits = m[0].replace(/\D/g, '');
    if (digits.length === 26 && !/^0+$/.test(digits)) hints.add(digits.slice(-4));
  }
  const layout = (
    t
      .replace(/^\uFEFF/, '')
      .split(/\r\n|\r|\n/)
      .find((l) => l.trim() !== '') ?? ''
  ).trim();
  return { from, to, accountHints: [...hints], layout };
}

/** Where a file goes: an existing account or a new one (by name). */
export type Target = { id: string } | { new: string };

/** The wizard's choices. */
export type Choices = {
  targets: Record<string, Target>; // legacy file id → account
  opening: Target | null;
  includeChats: boolean;
};

const ref = (t: Target) => ('id' in t ? { id: t.id } : { new: { name: t.new.trim() } });

/** The private data sent: everything but the legacy AI key. */
function privateOf(p: Obj): Obj {
  const out: Obj = {};
  for (const k of KNOWN_PRIVATE) if (k !== 'aiKey' && p[k] !== undefined) out[k] = p[k];
  return out;
}

/** The manifest of POST /legacy-imports and the file parts, in the manifest's order. */
export function buildManifest(o: Opened, c: Choices): { manifest: Obj; files: Blob[] } {
  const files = o.files.filter((f) => c.targets[f.id]);
  const opts = isObj(o.prefs['opts']) ? o.prefs['opts'] : {};
  const manifest: Obj = {
    version: 1,
    source: o.source,
    created: o.created,
    opts,
    files: files.map((f) => ({
      legacyId: f.id,
      name: f.name,
      added: f.added,
      ...(f.cfg ? { cfg: f.cfg } : {}),
      account: ref(c.targets[f.id] as Target),
    })),
    private: privateOf(o.private),
    ...(c.opening ? { opening: ref(c.opening) } : {}),
    ...(c.includeChats && o.chats ? { chats: o.chats } : {}),
    unknownKeys: o.unknownKeys,
  };
  return { manifest, files: files.map((f) => new Blob([f.data as BlobPart])) };
}

/** The legacy appearance and layout settings (never the AI address or key). */
export function legacyPrefs(prefs: Obj): Obj {
  const out: Obj = {};
  for (const k of ['theme', 'ui', 'vizH', 'noViz', 'mode', 'cards', 'cardOrder', 'catMode', 'cfg', 'sbOpen']) {
    if (prefs[k] !== undefined) out[k] = prefs[k];
  }
  const ai = prefs['ai'];
  if (isObj(ai)) {
    const a: Obj = {};
    for (const k of ['mode', 'think', 'maxRows', 'scope', 'width', 'max']) if (ai[k] !== undefined) a[k] = ai[k];
    out['ai'] = a;
  }
  return out;
}

/** Counts shown before the migration starts. */
export function summary(o: Opened) {
  const p = o.private;
  const count = (x: unknown) => (isObj(x) ? Object.keys(x).length : Array.isArray(x) ? x.length : 0);
  return {
    files: o.files.length,
    groups: count(p['groups']),
    labels: count(p['tags']),
    budgets: count(p['budgets']),
    decisions: count(p['dupKeep']) + count(p['dupPick']),
    chats: o.chats?.length ?? 0,
    investments: isObj(p['inv']),
    opening: typeof p['opening'] === 'number' && p['opening'] !== 0 ? p['opening'] : 0,
  };
}
