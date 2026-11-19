import { describe, expect, it } from 'vitest';

import { MAX_VERSION, capacity, encodeQr, errorCorrection, qrPath } from './qr';
import fixtures from './qr.fixtures.json';

const rowsOf = (modules: boolean[][]) => modules.map((row) => row.map((d) => (d ? '1' : '0')).join(''));

// The mask a reference symbol was made with, read from its format bits (the copy beside the
// bottom-left and top-right finders), and its error-correction level.
function formatOf(rows: string[]) {
  const n = rows.length;
  const at = (x: number, y: number) => (rows[y]?.[x] === '1' ? 1 : 0);
  let bits = 0;
  for (let i = 0; i < 8; i++) bits |= at(n - 1 - i, 8) << i;
  for (let i = 8; i < 15; i++) bits |= at(8, n - 15 + i) << i;
  bits ^= 0x5412;
  return { level: bits >>> 13, mask: (bits >>> 10) & 7 };
}

describe('the QR encoder', () => {
  it('has reference symbols for small, middle and the largest versions', () => {
    const versions = fixtures.map((f) => f.version);
    expect(Math.min(...versions)).toBe(1);
    expect(Math.max(...versions)).toBe(MAX_VERSION);
    expect(new Set(versions).size).toBeGreaterThanOrEqual(8);
    expect(versions.some((v) => v >= 7 && v < 10)).toBe(true); // version information, 8-bit length
    expect(versions.some((v) => v >= 10)).toBe(true); // 16-bit length
  });

  it.each(fixtures.map((f) => [f.version, f.text.length, f] as const))(
    'makes the symbol libqrencode makes, module for module (version %i, %i characters)',
    (_version, _length, f) => {
      const ref = formatOf(f.rows);
      expect(ref.level).toBe(0); // level M
      const forced = encodeQr(f.text, ref.mask);
      expect(forced?.version).toBe(f.version);
      expect(forced?.size).toBe(f.rows.length);
      expect(rowsOf(forced?.modules ?? [])).toEqual(f.rows);
    },
  );

  it('is checked against a reference for each of the eight masks', () => {
    expect([...new Set(fixtures.map((f) => formatOf(f.rows).mask))].sort()).toEqual([0, 1, 2, 3, 4, 5, 6, 7]);
  });

  // libqrencode scores masks a little differently from the standard, so the two don't always
  // choose the same one. Every mask gives a valid symbol; ours is the one with the lowest penalty
  // by the standard's rules, and it is the same symbol as that mask forced (which the references
  // above prove for each mask).
  it.each(fixtures.map((f) => [f.version, f] as const))('chooses a mask by itself (version %i)', (_v, f) => {
    const auto = encodeQr(f.text);
    if (!auto) throw new Error('no symbol');
    expect(auto.mask).toBeGreaterThanOrEqual(0);
    expect(auto.mask).toBeLessThanOrEqual(7);
    expect(formatOf(rowsOf(auto.modules))).toEqual({ level: 0, mask: auto.mask });
    expect(rowsOf(auto.modules)).toEqual(rowsOf(encodeQr(f.text, auto.mask)?.modules ?? []));
  });

  it('picks the smallest version that holds the text, and gives up beyond the largest', () => {
    expect(capacity(1)).toBe(14);
    expect(capacity(9)).toBe(180);
    expect(capacity(10)).toBe(213);
    expect(capacity(MAX_VERSION)).toBe(412);
    expect(encodeQr('a'.repeat(14))?.version).toBe(1);
    expect(encodeQr('a'.repeat(15))?.version).toBe(2);
    expect(encodeQr('a'.repeat(180))?.version).toBe(9);
    expect(encodeQr('a'.repeat(181))?.version).toBe(10);
    expect(encodeQr('é'.repeat(7))?.version).toBe(1); // 14 bytes
    expect(encodeQr('é'.repeat(8))?.version).toBe(2); // 16 bytes: bytes count, not characters
    expect(encodeQr('a'.repeat(412))?.version).toBe(MAX_VERSION);
    expect(encodeQr('a'.repeat(413))).toBeNull();
    expect(encodeQr('')?.version).toBe(1);
  });

  it("computes the Reed–Solomon codewords of the standard's example", () => {
    // ISO/IEC 18004, annex I: "01234567" in a version 1-M symbol
    const data = [0x10, 0x20, 0x0c, 0x56, 0x61, 0x80, 0xec, 0x11, 0xec, 0x11, 0xec, 0x11, 0xec, 0x11, 0xec, 0x11];
    expect(errorCorrection(data, 10)).toEqual([0xa5, 0x24, 0xd4, 0xc1, 0xed, 0x36, 0xc7, 0x87, 0x2c, 0x55]);
  });

  it('draws every dark module once in the path, inside the quiet zone', () => {
    const qr = encodeQr('Finuance');
    if (!qr) throw new Error('no symbol');
    const path = qrPath(qr, 4);
    let drawn = 0;
    for (const m of path.matchAll(/M(\d+) (\d+)h(\d+)v1h-(\d+)z/g)) {
      const [x, y, w] = [Number(m[1]), Number(m[2]), Number(m[3])];
      expect(x).toBeGreaterThanOrEqual(4);
      expect(y).toBeGreaterThanOrEqual(4);
      expect(x + w).toBeLessThanOrEqual(qr.size + 4);
      for (let i = 0; i < w; i++) expect(qr.modules[y - 4]?.[x - 4 + i]).toBe(true);
      drawn += w;
    }
    expect(drawn).toBe(qr.modules.flat().filter(Boolean).length);
    expect(path.replace(/M\d+ \d+h\d+v1h-\d+z/g, '')).toBe('');
  });
});
