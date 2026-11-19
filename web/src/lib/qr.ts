// A small QR code encoder (ISO/IEC 18004) for one purpose: showing the otpauth:// address of
// two-step sign-in so an authenticator app can scan it (PLAN 23.7, ADR 0040). It exists because a
// QR library would be a new dependency for one screen. Scope, on purpose: byte mode only (any
// UTF-8 text), error correction level M only, versions 1 to 15 (up to 412 bytes; an address is
// about 150). Longer text gives null, and the caller shows the text alone.
//
// Tested against matrices made by libqrencode (qr.test.ts), module for module.

/** A QR code: `modules[y][x]` is true for a dark module; `size` modules a side, without the quiet zone. */
export interface QrCode {
  version: number;
  mask: number;
  size: number;
  modules: boolean[][];
}

// Level M, per version: error-correction codewords per block, then [blocks, data codewords per
// block] for the first group and, where there is one, the second (ISO/IEC 18004, table 9).
const BLOCKS: readonly (readonly number[])[] = [
  [],
  [10, 1, 16],
  [16, 1, 28],
  [26, 1, 44],
  [18, 2, 32],
  [24, 2, 43],
  [16, 4, 27],
  [18, 4, 31],
  [22, 2, 38, 2, 39],
  [22, 3, 36, 2, 37],
  [26, 4, 43, 1, 44],
  [30, 1, 50, 4, 51],
  [22, 6, 36, 2, 37],
  [22, 8, 37, 1, 38],
  [24, 4, 40, 5, 41],
  [24, 5, 41, 5, 42],
];
// Centres of the alignment patterns, per version (annex E).
const ALIGNMENT: readonly (readonly number[])[] = [
  [],
  [],
  [6, 18],
  [6, 22],
  [6, 26],
  [6, 30],
  [6, 34],
  [6, 22, 38],
  [6, 24, 42],
  [6, 26, 46],
  [6, 28, 50],
  [6, 30, 54],
  [6, 32, 58],
  [6, 34, 62],
  [6, 26, 46, 66],
  [6, 26, 48, 70],
];
export const MAX_VERSION = BLOCKS.length - 1;

function blockSizes(version: number): { ec: number; data: number[] } {
  const [ec = 0, n1 = 0, d1 = 0, n2 = 0, d2 = 0] = BLOCKS[version] ?? [];
  return { ec, data: [...Array<number>(n1).fill(d1), ...Array<number>(n2).fill(d2)] };
}

/** How many bytes of text a version holds (byte mode, level M). */
export function capacity(version: number): number {
  const total = blockSizes(version).data.reduce((a, b) => a + b, 0);
  const header = 4 + (version < 10 ? 8 : 16); // the mode, then the length
  return Math.floor((total * 8 - header) / 8);
}

// Arithmetic in GF(256) with the QR polynomial x^8 + x^4 + x^3 + x^2 + 1.
function gfMul(a: number, b: number): number {
  let z = 0;
  for (let i = 7; i >= 0; i--) {
    z = (z << 1) ^ ((z >>> 7) * 0x11d);
    z ^= ((b >>> i) & 1) * a;
  }
  return z;
}

/** The Reed–Solomon remainder of `data` for `degree` error-correction codewords. */
export function errorCorrection(data: readonly number[], degree: number): number[] {
  // the generator polynomial (x - 2^0)(x - 2^1)…(x - 2^(degree-1)), without its leading 1
  const gen = Array<number>(degree).fill(0);
  gen[degree - 1] = 1;
  let root = 1;
  for (let i = 0; i < degree; i++) {
    for (let j = 0; j < degree; j++) {
      gen[j] = gfMul(gen[j] ?? 0, root) ^ (gen[j + 1] ?? 0);
    }
    root = gfMul(root, 2);
  }
  const rem = Array<number>(degree).fill(0);
  for (const byte of data) {
    const factor = byte ^ (rem.shift() ?? 0);
    rem.push(0);
    for (let i = 0; i < degree; i++) rem[i] = (rem[i] ?? 0) ^ gfMul(gen[i] ?? 0, factor);
  }
  return rem;
}

/** The codewords of the symbol: the text's data, padded, split into blocks, each with its error correction, interleaved. */
function codewords(bytes: Uint8Array, version: number): number[] {
  const { ec, data: sizes } = blockSizes(version);
  const total = sizes.reduce((a, b) => a + b, 0);
  const bits: number[] = [];
  const push = (value: number, length: number) => {
    for (let i = length - 1; i >= 0; i--) bits.push((value >>> i) & 1);
  };
  push(0b0100, 4); // byte mode
  push(bytes.length, version < 10 ? 8 : 16);
  for (const b of bytes) push(b, 8);
  push(0, Math.min(4, total * 8 - bits.length)); // the terminator
  push(0, (8 - (bits.length % 8)) % 8);
  const data: number[] = [];
  for (let i = 0; i < bits.length; i += 8) data.push(Number.parseInt(bits.slice(i, i + 8).join(''), 2));
  for (let pad = 0xec; data.length < total; pad ^= 0xec ^ 0x11) data.push(pad);

  const blocks: number[][] = [];
  const eccs: number[][] = [];
  let at = 0;
  for (const size of sizes) {
    const block = data.slice(at, at + size);
    at += size;
    blocks.push(block);
    eccs.push(errorCorrection(block, ec));
  }
  const out: number[] = [];
  const longest = Math.max(...sizes);
  for (let i = 0; i < longest; i++) for (const b of blocks) if (i < b.length) out.push(b[i] ?? 0);
  for (let i = 0; i < ec; i++) for (const e of eccs) out.push(e[i] ?? 0);
  return out;
}

const MASKS: readonly ((x: number, y: number) => boolean)[] = [
  (x, y) => (x + y) % 2 === 0,
  (_, y) => y % 2 === 0,
  (x) => x % 3 === 0,
  (x, y) => (x + y) % 3 === 0,
  (x, y) => (Math.floor(x / 3) + Math.floor(y / 2)) % 2 === 0,
  (x, y) => ((x * y) % 2) + ((x * y) % 3) === 0,
  (x, y) => (((x * y) % 2) + ((x * y) % 3)) % 2 === 0,
  (x, y) => (((x + y) % 2) + ((x * y) % 3)) % 2 === 0,
];

class Grid {
  readonly size: number;
  readonly dark: boolean[][];
  readonly fixed: boolean[][]; // function patterns: never data, never masked

  constructor(readonly version: number) {
    this.size = version * 4 + 17;
    this.dark = Array.from({ length: this.size }, () => Array<boolean>(this.size).fill(false));
    this.fixed = Array.from({ length: this.size }, () => Array<boolean>(this.size).fill(false));
  }

  get(x: number, y: number): boolean {
    return this.dark[y]?.[x] ?? false;
  }

  private put(x: number, y: number, dark: boolean, fixed: boolean) {
    const row = this.dark[y];
    const frow = this.fixed[y];
    if (!row || !frow || x < 0 || x >= this.size) return;
    row[x] = dark;
    if (fixed) frow[x] = true;
  }

  private fn(x: number, y: number, dark: boolean) {
    this.put(x, y, dark, true);
  }

  patterns() {
    const n = this.size;
    for (let i = 0; i < n; i++) {
      this.fn(6, i, i % 2 === 0); // timing
      this.fn(i, 6, i % 2 === 0);
    }
    for (const [cx, cy] of [
      [3, 3],
      [n - 4, 3],
      [3, n - 4],
    ] as const) {
      // a finder with its light separator: rings at distance 0–1 and 3 are dark
      for (let dy = -4; dy <= 4; dy++) {
        for (let dx = -4; dx <= 4; dx++) {
          const d = Math.max(Math.abs(dx), Math.abs(dy));
          this.fn(cx + dx, cy + dy, d !== 2 && d !== 4);
        }
      }
    }
    const centres = ALIGNMENT[this.version] ?? [];
    const last = centres.length - 1;
    centres.forEach((cy, i) => {
      centres.forEach((cx, j) => {
        if ((i === 0 && j === 0) || (i === 0 && j === last) || (i === last && j === 0)) return; // a finder is there
        for (let dy = -2; dy <= 2; dy++) {
          for (let dx = -2; dx <= 2; dx++) this.fn(cx + dx, cy + dy, Math.max(Math.abs(dx), Math.abs(dy)) !== 1);
        }
      });
    });
    this.format(0); // reserves the modules; the real bits come with the mask
    this.versionInfo();
  }

  /** The 15 format bits (level M and the mask, with their BCH code), twice. */
  format(mask: number) {
    const data = mask; // level M is 00 in the two bits above the mask
    let rem = data;
    for (let i = 0; i < 10; i++) rem = (rem << 1) ^ ((rem >>> 9) * 0x537);
    const bits = ((data << 10) | rem) ^ 0x5412;
    const bit = (i: number) => ((bits >>> i) & 1) === 1;
    const n = this.size;
    for (let i = 0; i <= 5; i++) this.fn(8, i, bit(i));
    this.fn(8, 7, bit(6));
    this.fn(8, 8, bit(7));
    this.fn(7, 8, bit(8));
    for (let i = 9; i < 15; i++) this.fn(14 - i, 8, bit(i));
    for (let i = 0; i < 8; i++) this.fn(n - 1 - i, 8, bit(i));
    for (let i = 8; i < 15; i++) this.fn(8, n - 15 + i, bit(i));
    this.fn(8, n - 8, true); // always dark
  }

  /** From version 7 on: the version number with its BCH code, twice. */
  private versionInfo() {
    if (this.version < 7) return;
    let rem = this.version;
    for (let i = 0; i < 12; i++) rem = (rem << 1) ^ ((rem >>> 11) * 0x1f25);
    const bits = (this.version << 12) | rem;
    for (let i = 0; i < 18; i++) {
      const dark = ((bits >>> i) & 1) === 1;
      const a = this.size - 11 + (i % 3);
      const b = Math.floor(i / 3);
      this.fn(a, b, dark);
      this.fn(b, a, dark);
    }
  }

  /** The codewords' bits in the zigzag from the bottom right, two columns at a time. */
  place(data: readonly number[]) {
    let i = 0;
    for (let right = this.size - 1; right >= 1; right -= 2) {
      if (right === 6) right = 5; // the vertical timing pattern's column is skipped
      for (let vert = 0; vert < this.size; vert++) {
        for (let j = 0; j < 2; j++) {
          const x = right - j;
          const upward = ((right + 1) & 2) === 0;
          const y = upward ? this.size - 1 - vert : vert;
          if (this.fixed[y]?.[x] !== false || i >= data.length * 8) continue;
          this.put(x, y, (((data[i >>> 3] ?? 0) >>> (7 - (i & 7))) & 1) === 1, false);
          i++;
        }
      }
    }
  }

  /** Applies a mask to the data modules; applying it again takes it off. */
  mask(mask: number) {
    const on = MASKS[mask];
    if (!on) throw new RangeError(`QR mask ${String(mask)}`);
    for (let y = 0; y < this.size; y++) {
      for (let x = 0; x < this.size; x++) {
        if (this.fixed[y]?.[x] === false && on(x, y)) this.put(x, y, !this.get(x, y), false);
      }
    }
  }

  /** The standard's penalty for a masked symbol: lower reads better. */
  penalty(): number {
    const n = this.size;
    let score = 0;
    const lines: boolean[][] = [];
    for (let i = 0; i < n; i++) {
      lines.push(Array.from({ length: n }, (_, x) => this.get(x, i)));
      lines.push(Array.from({ length: n }, (_, y) => this.get(i, y)));
    }
    for (const line of lines) {
      // runs of five or more of one colour
      let run = 1;
      for (let i = 1; i <= n; i++) {
        if (i < n && line[i] === line[i - 1]) {
          run++;
        } else {
          if (run >= 5) score += run - 2;
          run = 1;
        }
      }
      // what looks like a finder: dark 1, light 1, dark 3, light 1, dark 1 with four light beside it
      const s = line.map((d) => (d ? '1' : '0')).join('');
      for (const pattern of ['10111010000', '00001011101']) {
        for (let at = s.indexOf(pattern); at !== -1; at = s.indexOf(pattern, at + 1)) score += 40;
      }
    }
    let dark = 0;
    for (let y = 0; y < n; y++) {
      for (let x = 0; x < n; x++) {
        const c = this.get(x, y);
        if (c) dark++;
        // blocks of two by two
        if (
          x + 1 < n &&
          y + 1 < n &&
          c === this.get(x + 1, y) &&
          c === this.get(x, y + 1) &&
          c === this.get(x + 1, y + 1)
        ) {
          score += 3;
        }
      }
    }
    // how far the share of dark modules is from a half, in steps of five percent
    score += Math.floor(Math.abs((dark * 20) / (n * n) - 10)) * 10;
    return score;
  }
}

/**
 * Encodes text as a QR code, or returns null when it is longer than the largest version here
 * holds. `mask` forces one of the eight masks (tests); otherwise the one with the lowest penalty.
 */
export function encodeQr(text: string, mask?: number): QrCode | null {
  const bytes = new TextEncoder().encode(text);
  let version = 1;
  while (version <= MAX_VERSION && capacity(version) < bytes.length) version++;
  if (version > MAX_VERSION) return null;

  const grid = new Grid(version);
  grid.patterns();
  grid.place(codewords(bytes, version));
  let best = mask ?? 0;
  if (mask === undefined) {
    let low = Infinity;
    for (let m = 0; m < MASKS.length; m++) {
      grid.mask(m);
      grid.format(m);
      const p = grid.penalty();
      if (p < low) [low, best] = [p, m];
      grid.mask(m); // off again
    }
  }
  grid.mask(best);
  grid.format(best);
  return { version, mask: best, size: grid.size, modules: grid.dark };
}

/** The dark modules as one SVG path, in a coordinate system of modules with a quiet zone of `quiet`. */
export function qrPath(qr: QrCode, quiet = 4): string {
  const parts: string[] = [];
  qr.modules.forEach((row, y) => {
    let x = 0;
    while (x < qr.size) {
      if (row[x] !== true) {
        x++;
        continue;
      }
      let end = x;
      while (end < qr.size && row[end] === true) end++;
      parts.push(`M${String(x + quiet)} ${String(y + quiet)}h${String(end - x)}v1h-${String(end - x)}z`);
      x = end;
    }
  });
  return parts.join('');
}
