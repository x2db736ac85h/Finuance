import { describe, expect, it } from 'vitest';

import { convertExport, parseDelimited, toJson, toText } from './exportFormats';

const CSV = '﻿Date;Payee;Amount\r\n01.03.2025;"Kino; Helios";-34,00\r\n02.03.2025;"He said ""hi""\r\nagain";12,50\r\n';

describe('the export in other formats', () => {
  it('reads the server’s CSV: the BOM, quoted delimiters, doubled quotes, a line break in a cell', () => {
    expect(parseDelimited(CSV)).toEqual([
      ['Date', 'Payee', 'Amount'],
      ['01.03.2025', 'Kino; Helios', '-34,00'],
      ['02.03.2025', 'He said "hi"\r\nagain', '12,50'],
    ]);
    expect(parseDelimited('a;b')).toEqual([['a', 'b']]);
    expect(parseDelimited('')).toEqual([]);
    expect(parseDelimited('a;\r\n')).toEqual([['a', '']]);
  });

  it('writes JSON keyed by the header, values as the CSV has them', () => {
    expect(JSON.parse(toJson(parseDelimited(CSV)))).toEqual([
      { Date: '01.03.2025', Payee: 'Kino; Helios', Amount: '-34,00' },
      { Date: '02.03.2025', Payee: 'He said "hi"\r\nagain', Amount: '12,50' },
    ]);
    expect(toJson([['A']])).toBe('[]\n');
  });

  it('writes an aligned table, one line a row', () => {
    expect(toText(parseDelimited(CSV))).toBe(
      [
        'Date        Payee               Amount',
        '----------  ------------------  ------',
        '01.03.2025  Kino; Helios        -34,00',
        '02.03.2025  He said "hi" again  12,50',
        '',
      ].join('\n'),
    );
    expect(toText([])).toBe('');
  });

  it('keeps the CSV as it is and renames the others', async () => {
    const blob = new Blob([CSV], { type: 'text/csv' });
    expect(await convertExport(blob, 'finuance-all-2025-03-20.csv', 'csv')).toEqual({
      blob,
      filename: 'finuance-all-2025-03-20.csv',
    });
    const json = await convertExport(blob, 'finuance-all-2025-03-20.csv', 'json');
    expect(json.filename).toBe('finuance-all-2025-03-20.json');
    expect(JSON.parse(await json.blob.text())).toHaveLength(2);
    const txt = await convertExport(blob, 'finuance-all-2025-03-20.csv', 'txt');
    expect(txt.filename).toBe('finuance-all-2025-03-20.txt');
    expect((await txt.blob.text()).split('\n')[0]).toMatch(/^Date\s+Payee\s+Amount$/);
  });
});
