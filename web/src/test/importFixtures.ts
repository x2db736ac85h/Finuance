// Small import previews for tests (the shape POST /imports and PATCH return).
import type { ImportPreview, StagedRow } from '../api/hooks/imports';

export const IMPORT_ID = '00000000-0000-4000-8000-0000000000f1';

export function stagedRow(i: number, over: Partial<StagedRow> = {}): StagedRow {
  return {
    rowIndex: i,
    bookedOn: `2025-01-${String((i % 28) + 1).padStart(2, '0')}`,
    amount: { amountMinor: -1000 - i, currency: 'PLN' },
    payee: `Sklep ${i}`,
    description: `Zakup ${i}`,
    reference: '',
    txType: '',
    bankCategory: 'Zakupy',
    counterpartyAccount: '',
    raw: [
      { header: 'Data', value: `0${(i % 9) + 1}.01.2025` },
      { header: 'Opis', value: `Sklep ${i}` },
      { header: 'Kwota', value: `-${10 + i},00` },
    ],
    duplicate: false,
    ...over,
  };
}

export function preview(over: Partial<ImportPreview> = {}, rows = 3): ImportPreview {
  const mapping = {
    bookedOn: { index: 0, header: 'Data' },
    payee: { index: 1, header: 'Opis' },
    amount: { index: 2, header: 'Kwota' },
    ignore: [],
  };
  return {
    import: {
      id: IMPORT_ID,
      filename: 'wyciag.csv',
      sizeBytes: 1234,
      status: 'previewed',
      mappingSource: 'detected',
      createdAt: '2026-10-22T10:00:00Z',
      stats: {},
      encoding: 'utf-8',
      delimiter: ';',
      headers: ['Data', 'Opis', 'Kwota'],
      columnTypes: ['date', 'text', 'number'],
      mapping,
      options: {
        encoding: 'auto',
        delimiter: 'auto',
        headerRow: 'auto',
        skipRows: 0,
        dropShort: true,
        dateOrder: 'dmy',
        stripQuote: true,
        invertSign: false,
        amountMode: 'single',
        currencyFallback: '',
      },
    },
    stats: {
      rows,
      errors: 0,
      duplicatesEstimate: 0,
      newEstimate: rows,
      headerAt: 1,
      dropped: 0,
      dateFrom: '2025-01-01',
      dateTo: '2025-01-03',
    },
    suggestedAccount: { createName: 'Konto …0001' },
    sampleRows: Array.from({ length: Math.min(rows, 50) }, (_, i) => stagedRow(i)),
    tailRows: rows > 55 ? Array.from({ length: 5 }, (_, i) => stagedRow(rows - 5 + i)) : [],
    missing: [],
    warnings: [],
    detected: mapping,
    ...over,
  };
}
