// Inputs for the value-parser corpora. Each list covers every branch of the legacy function; the
// oracle records the legacy output for every input. Combinations are expanded from small hand-written
// lists so the branch coverage stays readable.

const cross = (as, bs, f) => as.flatMap(a => bs.map(b => f(a, b)));

export const parseNum = [
  // plain integers and decimals
  '0', '1', '-1', '+1', '42', '-42', '1234', '-1234', '999999999999999', '1000000000000000', '0.5', '-0.5', '0,5', '-0,5',
  '12.34', '-12.34', '12,34', '-12,34', '.5', ',5', '5.', '5,', '-.5',
  // thousands separators
  '1 234', '-1 234,56', '1 234 567,89', '1 234,56', '1 234,56', "1'234.56", '1.234,56', '-1.234,56', '1,234.56',
  '-1,234.56', '1.234.567', '1,234,567', '1.234.567,89', '1,234,567.89', '12.345.678,90', '1 2 3', '12 34,5',
  // currency prefixes and suffixes (the regex is anchored at one end only)
  '12,00 PLN', '-12,00 PLN', '12,00PLN', 'PLN 12,00', 'PLN12', '€12', '-€12', '€-12', '12€', '12 €', '$5.99', '£3', '3£',
  '12 zł', '12zł', '-7,50 zł', 'zł 7', 'pln 12', '12 eur', 'EUR12,50', 'USD 1,234.56', 'CHF 99', 'GBP-3', '12 PLNX',
  // signs and spacing
  ' 12 ', '\t12\t', '- 12', '+ 12', '--12', '+-12', '12-', '-', '+', '', ' ', '.', ',', '..', ',,',
  // rejected: too many digits, leading zeros, letters
  '1234567890123456', '12345678901234567', '-1234567890123456', '123456789012345.6', '0123', '-0123', '00', '007', '0,50',
  '00,50', '0.0', '-0', '12a', 'a12', '1e3', '1E3', 'Infinity', 'NaN', '0x1F', '12 34 56 78', '1/2', '12%', '(12)',
  // ambiguous separators
  '1,2', '1,23', '1,234', '1,2345', '1.2', '1.23', '1.234', '1.2345', '1,234.5', '1.234,5', '1,2.3', '1.2,3', '1,,2', '1..2',
  // realistic bank values
  '-127,13', '-44,98', '-4,40', '-40,00', '-200,00', '-29,29', '-235,00', '9 200,00', '-2 850,00', '-79,99', '8 500,00',
  '-800,00 PLN', '284,30 PLN', '-5 521,28', '584,30', '-43.52', '-10', '-265.13', '-1600', '3200', '0.00', '43.52',
  // account-like and reference-like strings
  "'32124023", '00000000000000000000000001', 'C992506002418351', '*********0229197', '86772307062', '000134679',
  // mixed
  '12,00 PLN ', ' -1 234,56 PLN', '−12', '12 PLN', '1 000', '10 000', '100 000', '1 000 000', '1,000', '10,000', '1.000'
];

const dates = ['2025-01-02', '2025-1-2', '2025/01/02', '2025.01.02', '2024-02-29', '2025-02-29', '2025-02-31', '2025-13-01',
  '2025-00-10', '2025-01-00', '2025-01-32', '0999-01-01', '9999-12-31'];
const dmy = ['02.01.2025', '2.1.2025', '02/01/2025', '02-01-2025', '31.12.2024', '31.02.2025', '29.02.2024', '29.02.2025',
  '32.01.2025', '00.01.2025', '01.13.2025', '01.00.2025', '10/31/2025', '1.1.25', '01.01.25'];
const times = ['', ' 07:21', ' 7:21', 'T16:40', 'T16:40:05', ' 23:59:59', ' 24:00', ' 12:60', ', 10:36', ',10:36', ' 10:36:5'];
export const parseDate = [
  ...cross(dates.slice(0, 6), times.slice(0, 6), (d, t) => d + t),
  ...cross(dates.slice(6), [' 07:21', 'T16:40'], (d, t) => d + t), ...dates.slice(6),
  ...cross(dmy.slice(0, 6), times, (d, t) => d + t),
  ...dmy.slice(6),
  '', ' ', '2025', '2025-03', '03.2025', 'abc', '15.04.2024 ', ' 15.04.2024', '15.04.2024x', '2025-01-02x', '2025-01-02 x',
  '20250102', '02012025', '2025-01-02T07:21Z', '2025-01-02T07:21:00.000Z', '2025-01-02 07:21:00+01:00', '1.2.2025, 10:36',
  'Data wykonania operacji: 2026-04-03', '2026-04-03 07:21 extra', '02.01.2025 07', '02 01 2025'
];

export const looseDate = [
  ...parseDate,
  '2025', '1999', '0001', '99999', '2025-03', '2025-3', '2025.03', '2025/03', '2025-12', '2025-13', '2025-00',
  '03.2025', '3.2025', '03-2025', '03/2025', '12.2025', '13.2025', '00.2025',
  ' 2025 ', ' 2025-03 ', '2025-03-15', '15.03.2025', '15.03.2025 10:00', '2025-03-15 10:00', '2025-03-15T10:00:30',
  '', ' ', 'x', '2025-', '-2025', '2025--03', '03..2025', 'Q1 2025', '2025-W10', 'March 2025', '2025-03-15..2025-03-20'
];

const words = ['PŁATNOŚĆ', 'płatność', 'Łódź', 'ŁÓDŹ', 'Zażółć gęślą jaźń', 'ZAŻÓŁĆ GĘŚLĄ JAŹŃ', 'Ąą Ćć Ęę Łł Ńń Óó Śś Źź Żż',
  'Café', 'naïve', 'Ångström', 'Straße', 'İstanbul', 'ǅemal', 'Øre', 'Æble', 'œuvre', 'ﬁnance', 'Ⅻ', '①', 'ｆｕｌｌ'];
export const norm = [
  ...words, ...words.map(w => w.toLowerCase()), ...words.map(w => '  ' + w + '\t'),
  '', ' ', 'a', 'A', '123', '-127,13', 'BLIK REF       86772307062', 'ZABKA Z6373 K.1        RASZYN', 'é', 'Ȩ́',
  'à́̂', 'l̵', 'ł́', 'Ł́', ' ', 'x​y', 'tab\there', 'new\nline', 'cr\rlf', 'emoji 💸',
  'ß', 'ẞ', 'ﬀ', 'Ǆ', 'ǆ', 'ı', 'İ', 'ŉ', 'ĳ', 'Ĳ', 'ð', 'Þ', 'đ', 'Đ', 'ħ', 'ŧ', 'ŀ', 'ſ', 'µ', 'Ω', 'Å', 'ǻ',
  'Ŭ', 'ǖ', 'ṩ', 'ộ', 'ừ', 'ỹ', 'ı̇', 'ģ', 'ķ', 'ļ', 'ņ', 'ŗ', 'ș', 'ț', 'ş', 'ţ', 'ğ', 'ž', 'š', 'č', 'ř', 'ď', 'ť', 'ň',
  'ů', 'ő', 'ű', 'ǎ', 'ǐ', 'ǒ', 'ǔ', 'ȁ', 'ȅ', 'ạ', 'ẹ', 'ị', 'ọ', 'ụ', 'Ạ', 'ĺ', 'ľ', 'ŕ', 'ŷ', 'ẁ', 'ẃ', 'ẅ', 'ÿ', 'Ÿ', 'ñ', 'Ñ', 'õ', 'ã', 'â', 'ê', 'î', 'ô', 'û'
];

export const payeeName = [
  'ZABKA Z6373 K.1        RASZYN', 'ZACHETA                WARSZAWA', 'UBER   * EATS PENDING  AMSTERDAM', 'IA *CEBULEKWIATOWE     CZESTOCHOWA',
  'AMAZON MARKETPLACE', 'PAYPRO S.A.', 'CITY-NAV SPOLKA Z OGRAN', '  LEADING SPACES', 'TRAILING SPACES  ', '  BOTH  ', '',
  ' ', '   ', 'A', 'A  B', 'A B', 'A\tB', 'A\t\tB', 'A \tB', 'A  B', 'A\nB', 'A\n\nB', 'A\r\nB', 'MULTI   SPACE   RUNS',
  'single space only here', 'X  ', '  X', 'one  two  three  four', 'Tab\tin\tmiddle', 'MARKET NA ROGU       WARSZAWA',
  'DELIKATESY POD LIPĄ  WARSZAWA', 'PIEKARNIA ZIARNO     WARSZAWA', 'KAWIARNIA PODWÓRKO   WARSZAWA', 'KOMUNIKACJA MIEJSKA',
  'STACJA PALIW POLNA   WARSZAWA', 'SKLEP INTERNETOWY ALFA', 'PRACODAWCA PRZYKŁADOWY SP. Z O.O.', 'WSPÓLNOTA MIESZKANIOWA',
  null, 0, 123, 'x'.repeat(200), 'NAME' + ' '.repeat(50) + 'CITY',
  ...Array.from({length: 110}, (_, i) => {
    const names = ['SKLEP', 'KAWIARNIA', 'Bistro', 'apteka', 'STACJA PALIW', 'Kino Pod Gwiazdami', 'BLIK', 'PRZELEW'];
    const cities = ['WARSZAWA', 'Kraków', 'ŁÓDŹ', 'gdańsk', 'POZNAŃ', ''];
    const pads = ['', ' ', '  ', '   ', '\t', '        ', '  \t  '];
    return `${i % 3 === 0 ? ' ' : ''}${names[i % names.length]} ${i}${pads[i % pads.length]}${cities[i % cities.length]}${i % 4 === 0 ? '  ' : ''}`;
  })
];
