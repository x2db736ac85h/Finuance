#!/usr/bin/env node
// Writes the synthetic CSV fixtures in testdata/fixtures/synthetic/ with exact bytes, and their README.
// Everything here is invented: no real people, accounts or merchants. Account-like numbers use the
// all-zero form that tools/check-private.sh allowlists.
//
// Usage: node tools/legacy-oracle/make-fixtures.mjs [--check]
//   --check  fail if any committed fixture differs from what this script would write (CI drift check)

import {mkdirSync, readFileSync, writeFileSync, existsSync, readdirSync, statSync, rmSync} from 'node:fs';
import {dirname, join, relative} from 'node:path';
import {fileURLToPath} from 'node:url';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..', '..');
const OUT = join(ROOT, 'testdata', 'fixtures', 'synthetic');
const CHECK = process.argv.includes('--check');

/* ---------- byte encoders (no dependencies) ---------- */
const PL = 'ĄąĆćĘęŁłŃńÓóŚśŹźŻż';
const TABLES = {
  'windows-1250': Object.fromEntries([...PL].map((c, i) => [c, [0xA5, 0xB9, 0xC6, 0xE6, 0xCA, 0xEA, 0xA3, 0xB3, 0xD1, 0xF1, 0xD3, 0xF3, 0x8C, 0x9C, 0x8F, 0x9F, 0xAF, 0xBF][i]])
    .concat([['€', 0x80], [' ', 0xA0], ['„', 0x84], ['”', 0x94], ['–', 0x96], ['ü', 0xFC], ['ä', 0xE4], ['é', 0xE9]])),
  'iso-8859-2': Object.fromEntries([...PL].map((c, i) => [c, [0xA1, 0xB1, 0xC6, 0xE6, 0xCA, 0xEA, 0xA3, 0xB3, 0xD1, 0xF1, 0xD3, 0xF3, 0xA6, 0xB6, 0xAC, 0xBC, 0xAF, 0xBF][i]])
    .concat([[' ', 0xA0], ['ü', 0xFC], ['ä', 0xE4], ['é', 0xE9]])),
  'windows-1252': Object.fromEntries([['€', 0x80], [' ', 0xA0], ['£', 0xA3], ['é', 0xE9], ['è', 0xE8], ['ä', 0xE4], ['ö', 0xF6], ['ü', 0xFC], ['ß', 0xDF], ['ç', 0xE7], ['ñ', 0xF1], ['Ö', 0xD6], ['Ü', 0xDC]])
};
function encode(text, enc){
  if (enc === 'utf-8') return Buffer.from(text, 'utf8');
  const t = TABLES[enc], out = [];
  for (const ch of text){
    const cp = ch.codePointAt(0);
    if (cp < 0x80) out.push(cp);
    else if (t[ch] != null) out.push(t[ch]);
    else throw new Error(`cannot encode ${JSON.stringify(ch)} in ${enc}`);
  }
  return Buffer.from(out);
}

/* ---------- fixture registry ---------- */
const F = [];
// lines: array of records (joined by eol), or a raw string (used verbatim, for mixed endings)
function fx(path, intent, lines, {enc = 'utf-8', eol = '\n', bom = false, final = true} = {}){
  F.push({path, intent, lines, enc, eol, bom, final});
}
const PEKAO_H = 'Data księgowania;Data waluty;Nadawca / Odbiorca;Adres nadawcy / odbiorcy;Rachunek źródłowy;Rachunek docelowy;Tytułem;Kwota operacji;Waluta;Numer referencyjny;Typ operacji;Kategoria';
const ACC1 = "'00000000000000000000000001", ACC2 = "'00000000000000000000000002";
const pekao = (d, v, who, addr, title, amt, ref, typ, cat) => [d, v, who, addr, ACC1, '', title, amt, 'PLN', ref, typ, cat].join(';');
const SIMPLE_H = 'Data;Opis;Kwota;Waluta;Kategoria';

/* -- encodings -- */
fx('01-utf8-bom.csv', 'UTF-8 with BOM; the BOM must not leak into the first header name.', [SIMPLE_H,
  '03.01.2025;Sklep spożywczy Żółw;-45,20;PLN;Artykuły spożywcze',
  '04.01.2025;Kawiarnia Łąka;-14,00;PLN;Restauracje i kawiarnie',
  '10.01.2025;Wynagrodzenie styczeń;8 500,00;PLN;Wynagrodzenie'], {bom: true});
fx('02-utf8-no-bom.csv', 'UTF-8 without BOM, Polish letters in every column type.', [SIMPLE_H,
  '03.01.2025;Księgarnia Ćma;-59,99;PLN;Książki',
  '05.01.2025;Źródło wody Ńa;-6,50;PLN;Artykuły spożywcze',
  '11.01.2025;Zwrot za bilet Gdańsk;120,00;PLN;Wpływy']);
fx('03-windows-1250.csv', 'Windows-1250 bytes with all 18 Polish letters; auto-detect must fall back from UTF-8 to Windows-1250.', [SIMPLE_H,
  '03.01.2025;ZAŻÓŁĆ GĘŚLĄ JAŹŃ;-12,34;PLN;Zakupy',
  '04.01.2025;zażółć gęślą jaźń;-56,78;PLN;Zakupy',
  '05.01.2025;Opłata „miesięczna” – karta;-10,00;PLN;Opłaty i odsetki'], {enc: 'windows-1250', eol: '\r\n'});
fx('04-iso-8859-2.csv', 'ISO-8859-2 bytes. Auto-detect decodes them as Windows-1250, so Ą/ą/Ś/ś/Ź/ź come out wrong; only the manual ISO-8859-2 option is correct.', [SIMPLE_H,
  '03.01.2025;ŚWIĘTOKRZYSKA ŹRÓDŁA ĄŁ;-21,00;PLN;Zakupy',
  '04.01.2025;świętokrzyska źródła ął;-22,00;PLN;Zakupy'], {enc: 'iso-8859-2'});
fx('05-windows-1252.csv', 'Windows-1252 (Western European): € sign and accented letters; manual option only.', ['Date;Description;Amount;Currency',
  '2025-02-01;Café Müller;-€12,50;EUR',
  '2025-02-02;Straße Brötchen;-3,20;EUR',
  '2025-02-03;Crème brulée;-7,00;EUR'], {enc: 'windows-1252', eol: '\r\n'});

/* -- structure -- */
fx('06-bank-preamble.csv', '5 lines of account information before the header; auto header = first row with the modal field count.', [
  'Wyciąg z rachunku', 'Rachunek: 00000000000000000000000001', 'Właściciel: JAN PRZYKŁADOWY', 'Okres: 01.01.2025 - 31.01.2025', 'Waluta: PLN',
  SIMPLE_H, '02.01.2025;Sklep A;-10,00;PLN;Zakupy', '03.01.2025;Sklep B;-20,00;PLN;Zakupy', '04.01.2025;Sklep C;-30,00;PLN;Zakupy']);
fx('07-trailing-delimiters.csv', 'Every line ends with ";;" — the resulting empty unnamed columns must be dropped.', [SIMPLE_H + ';;',
  '02.01.2025;Sklep A;-10,00;PLN;Zakupy;;', '03.01.2025;Sklep B;-20,00;PLN;Zakupy;;']);
fx('08-quoted-fields.csv', 'Quoted fields containing the delimiter, doubled quotes "" and an embedded CRLF newline.', [
  'Data;Opis;Kwota', '02.01.2025;"Sklep; filia nr 2";-10,00', '03.01.2025;"Księgarnia ""Pod Sową""";-20,00',
  '04.01.2025;"Linia pierwsza\r\nlinia druga";-30,00', '05.01.2025;"";-40,00']);
fx('09-crlf.csv', 'CRLF line endings.', [SIMPLE_H, '02.01.2025;Sklep A;-10,00;PLN;Zakupy', '03.01.2025;Sklep B;-20,00;PLN;Zakupy'], {eol: '\r\n'});
fx('10-lf.csv', 'LF line endings, no final newline.', [SIMPLE_H, '02.01.2025;Sklep A;-10,00;PLN;Zakupy', '03.01.2025;Sklep B;-20,00;PLN;Zakupy'], {final: false});
fx('11-cr.csv', 'Classic Mac CR-only line endings.', [SIMPLE_H, '02.01.2025;Sklep A;-10,00;PLN;Zakupy', '03.01.2025;Sklep B;-20,00;PLN;Zakupy'], {eol: '\r'});
fx('12-mixed-line-endings.csv', 'CRLF, LF and CR mixed in one file.', 'Data;Opis;Kwota\r\n02.01.2025;A;-1,00\n03.01.2025;B;-2,00\r04.01.2025;C;-3,00\r\n');
fx('13-tab-delimited.csv', 'Tab-delimited, amounts with comma decimals.', ['Data\tOpis\tKwota\tWaluta', '02.01.2025\tSklep A\t-10,00\tPLN', '03.01.2025\tSklep, filia\t-20,00\tPLN']);
fx('14-pipe-delimited.csv', 'Pipe-delimited, ISO dates, dot decimals.', ['Date|Description|Amount|Currency', '2025-01-02|Shop A|-10.00|PLN', '2025-01-03|Shop B|-20.50|PLN']);
fx('15-comma-quoted-decimals.csv', 'Comma-delimited with quoted comma-decimal amounts ("-1 234,56").', ['Data,Opis,Kwota,Waluta',
  '02.01.2025,Sklep A,"-1 234,56",PLN', '03.01.2025,"Sklep B, Warszawa","-20,00",PLN', '04.01.2025,Wpływ,"3 000,00",PLN']);
fx('16-delimiter-ambiguity.csv', 'Semicolon file whose unquoted addresses contain varying numbers of commas; consistency must beat raw comma count.', [
  'Data;Kontrahent;Adres;Kwota', '02.01.2025;Sklep A;ul. Długa 1, 00-001 Warszawa, PL;-10,00', '03.01.2025;Sklep B;ul. Krótka 2, Kraków;-20,00',
  '04.01.2025;Sklep C;Rynek 3;-30,00', '05.01.2025;Sklep D;al. Polna 4, lok. 5, 00-002, Łódź, PL;-40,00']);
fx('17-amount-formats.csv', 'Every number format parseNum accepts plus near-misses that must stay text-like.', ['Data;Opis;Kwota',
  '01.01.2025;space thousands;-1 234,56', '02.01.2025;dot thousands;1.234,56', '03.01.2025;comma thousands;1,234.56',
  '04.01.2025;PLN suffix;12,00 PLN', '05.01.2025;euro prefix;€12', '06.01.2025;nbsp thousands;-2 500,00',
  '07.01.2025;plus sign;+15,00', '08.01.2025;zł suffix;-7,50 zł', '09.01.2025;integer;-10', '10.01.2025;many dots;1.234.567']);
fx('18-apostrophe-accounts.csv', "Excel text marker: account numbers and references prefixed with ' (stripped by default).", [
  'Data;Rachunek;Referencja;Kwota', `02.01.2025;${ACC1};'C000000000000001;-10,00`, `03.01.2025;${ACC2};'C000000000000002;-20,00`]);
fx('19-leading-zero-and-long-ids.csv', 'Leading-zero ids and 16+ digit references must stay text, never numbers.', ['Data;Id;Referencja;Kwota',
  '02.01.2025;0012345;1234567890123456;-10,00', '03.01.2025;0000042;98765432109876543;-20,00', '04.01.2025;0099;12345678901234567890;-30,00']);
fx('20-duplicate-headers.csv', 'Two columns named "Kwota" and two named "Opis": renamed "Kwota (2)", "Opis (2)".', ['Data;Opis;Kwota;Opis;Kwota',
  '02.01.2025;A;-10,00;a;-1,00', '03.01.2025;B;-20,00;b;-2,00']);
fx('21-blank-headers.csv', 'Blank header cells with data below: named "Column N".', ['Data;;Kwota;', '02.01.2025;x;-10,00;y', '03.01.2025;z;-20,00;w']);
fx('22-ragged-rows.csv', 'Rows with fewer and more fields than the header (padded; extra columns named).', ['Data;Opis;Kwota;Waluta',
  '02.01.2025;A;-10,00;PLN', '03.01.2025;B;-20,00', '04.01.2025;C;-30,00;PLN;extra', '05.01.2025;D;-40,00;PLN']);
fx('23-dates-iso.csv', 'ISO yyyy-mm-dd dates.', ['Date;Description;Amount', '2025-01-02;A;-10,00', '2025-01-31;B;-20,00', '2024-02-29;Leap;-30,00']);
fx('24-dates-dmy-dots.csv', 'dd.mm.yyyy dates, including single-digit day/month.', ['Data;Opis;Kwota', '02.01.2025;A;-10,00', '1.2.2025;B;-20,00', '31.12.2024;C;-30,00']);
fx('25-dates-dmy-slash.csv', 'dd/mm/yyyy and dd-mm-yyyy dates (day-first).', ['Data;Opis;Kwota', '02/01/2025;A;-10,00', '13/01/2025;B;-20,00', '14-01-2025;C;-30,00']);
fx('26-dates-with-times.csv', 'Dates with times: ISO with T and space, day-first with comma.', ['Data;Opis;Kwota', '2025-01-02 07:21;A;-10,00',
  '2025-01-03T16:40:05;B;-20,00', '04.01.2025 18:26;C;-30,00', '05.01.2025, 10:36;D;-40,00']);
fx('27-invalid-date.csv', 'Impossible date 31.02.2025 among valid ones (legacy rolls it over to March; the rebuild rejects it).', ['Data;Opis;Kwota',
  '28.02.2025;A;-10,00', '31.02.2025;Invalid;-20,00', '01.03.2025;B;-30,00', '32.01.2025;Out of range;-40,00']);
fx('28-debit-credit.csv', 'Separate debit and credit columns instead of one signed amount.', ['Data;Opis;Obciążenia;Uznania',
  '02.01.2025;Sklep;45,20;', '10.01.2025;Wynagrodzenie;;8 500,00', '11.01.2025;Kawiarnia;14,00;']);
fx('29-positive-expenses.csv', 'Expenses exported as positive numbers (needs invertSign).', ['Date;Description;Amount', '2025-01-02;Shop;45.20', '2025-01-03;Cafe;14.00', '2025-01-10;Refund;-20.00']);
fx('30-empty.csv', 'Zero bytes.', '');
fx('31-header-only.csv', 'Header line and nothing else.', [SIMPLE_H]);
fx('32-whitespace-lines.csv', 'Blank and whitespace-only lines between records.', [SIMPLE_H, '', '02.01.2025;A;-10,00;PLN;Zakupy', '   ', '\t', '03.01.2025;B;-20,00;PLN;Zakupy', '']);
fx('33-quote-mid-field.csv', 'A quote inside an unquoted field is literal (quotes only open at field start).', ['Data;Opis;Kwota', '02.01.2025;Sklep 5" ekran;-10,00', '03.01.2025;Pod "Lipą" bar;-20,00']);
fx('34-unterminated-quote.csv', 'A quoted field that is never closed swallows the rest of the file.', ['Data;Opis;Kwota', '02.01.2025;"Otwarty cudzysłów;-10,00', '03.01.2025;B;-20,00']);

/* -- v7 import-settings cases -- */
fx('35-padded-preamble.csv', 'Preamble lines padded with empty fields ("Bank export;;;") have the modal field count but must be skipped by the fill-ratio rule.', [
  'Bank export;;;;', 'Konto;00000000000000000000000001;;;', ';;;;', SIMPLE_H, '02.01.2025;A;-10,00;PLN;Zakupy', '03.01.2025;B;-20,00;PLN;Zakupy', '04.01.2025;C;-30,00;PLN;Zakupy']);
fx('36-footer-summary.csv', 'Footer summary lines with far fewer fields ("Saldo końcowe;1234,56") are dropped (dropShort).', [SIMPLE_H,
  '02.01.2025;A;-10,00;PLN;Zakupy', '03.01.2025;B;-20,00;PLN;Zakupy', '04.01.2025;C;1 264,56;PLN;Wpływy', 'Saldo końcowe;1234,56', 'Liczba operacji;3']);
fx('37-headerless.csv', 'No header row at all: every record is data (needs header = none).', ['02.01.2025;Sklep A;-10,00;PLN', '03.01.2025;Sklep B;-20,00;PLN', '04.01.2025;Sklep C;-30,00;PLN']);
fx('38-header-on-record-7.csv', 'Header on record 7, after six free-form lines with varying field counts.', ['Raport operacji', 'Bank Przykładowy S.A.', 'Klient;JAN PRZYKŁADOWY',
  'Okres;01.01.2025;31.01.2025', 'Saldo początkowe;1 000,00;PLN', 'Wygenerowano;2025-02-01;10:00;UTC', SIMPLE_H,
  '02.01.2025;A;-10,00;PLN;Zakupy', '03.01.2025;B;-20,00;PLN;Zakupy']);
fx('39-month-first-dates.csv', 'US month-first dates (10/31/2025); ambiguous ones like 03/04/2025 need dateOrder = mdy.', ['Date;Description;Amount',
  '10/31/2025;Halloween shop;-25.00', '03/04/2025;Ambiguous;-10.00', '12/25/2025;Gift;-99.99']);
fx('40-junk-column.csv', 'An internal junk column (row hash) that should be mapped to ignore.', ['Data;Opis;Kwota;Hash',
  '02.01.2025;A;-10,00;9f8e7d6c', '03.01.2025;B;-20,00;1a2b3c4d']);
fx('41-debit-credit-and-signed.csv', 'Debit/credit columns alongside a signed amount column (mapping keeps both as plain columns).', ['Data;Opis;Kwota;Wydatki;Wpływy',
  '02.01.2025;Sklep;-45,20;45,20;', '10.01.2025;Wynagrodzenie;8 500,00;;8 500,00']);
fx('42-bank-a-layout.csv', 'Bank A layout (Polish headers) — maps to the same canonical columns as 43-bank-b-layout.csv for merged-view parity.', [
  'Data operacji;Kontrahent;Tytuł;Kwota;Waluta;Kategoria', '02.01.2025;SKLEP ALFA;Zakupy;-10,00;PLN;Zakupy', '03.01.2025;KAWIARNIA BETA;Kawa;-12,00;PLN;Restauracje']);
fx('43-bank-b-layout.csv', 'Bank B layout (English headers, ISO dates, dot decimals, different column order).', [
  'Category,Amount,Currency,Booking date,Payee,Title', 'Zakupy,-15.00,PLN,2025-01-04,SKLEP GAMMA,Zakupy', 'Restauracje,-12.00,PLN,2025-01-03,KAWIARNIA BETA,Kawa'], {eol: '\r\n'});
fx('44-single-column.csv', 'A single column with no delimiter anywhere (detectDelim falls back to comma).', ['Opis', 'A', 'B', 'C']);
fx('45-numeric-looking-dates.csv', 'A column of dd.mm.yyyy values is a date, not a number (dates are checked before numbers).', ['Data;Kwota', '15.04.2024;-10,00', '16.04.2024;-20,00']);
fx('46-mostly-numbers.csv', 'Typing threshold: a column with 9/10 numeric values is number; with 8/10 it is text.', ['Data;Ninety;Eighty',
  ...Array.from({length: 10}, (_, i) => `${String(i + 1).padStart(2, '0')}.01.2025;${i === 0 ? 'n/a' : (i + 1) * 10 + ',00'};${i < 2 ? 'n/a' : (i + 1) * 10 + ',00'}`)]);
fx('47-balance-column.csv', 'Includes a balance (saldo) column that must not be picked as the amount role.', ['Data;Opis;Saldo;Kwota',
  '02.01.2025;A;990,00;-10,00', '03.01.2025;B;970,00;-20,00']);
fx('48-reference-column.csv', 'Has a reference column (dedupe key uses ref + amount).', ['Data;Opis;Numer referencyjny;Kwota',
  '02.01.2025;A;REF-0001;-10,00', '02.01.2025;A;REF-0002;-10,00', '03.01.2025;B;REF-0003;-20,00']);

/* -- bank look-alikes (structure copied from real exports, every value invented) -- */
fx('banks/pekao-like.csv', 'Pekao layout: UTF-8, LF, ";", dd.mm.yyyy, comma decimals, \' account/reference prefixes, payee padded with spaces before the city.', [PEKAO_H,
  pekao('01.03.2025', '01.03.2025', 'SKLEP INTERNETOWY ALFA', 'ALFA.PL UL. PRZYKŁADOWA 1,WARSZAWA', 'BLIK REF       10000000001', '-127,13', "'C000000000000001", 'PŁATNOŚĆ BLIK', 'Zakupy przez internet'),
  pekao('01.03.2025', '28.02.2025', 'MINIMARKET Z1001 K.1   GDANSK', '', '*********0000001', '-44,98', "'C000000000000002", 'TRANSAKCJA KARTĄ PŁATNICZĄ', 'Artykuły spożywcze'),
  pekao('28.02.2025', '28.02.2025', 'GALERIA SZTUKI         WARSZAWA', '', '*********0000001', '-40,00', "'C000000000000003", 'TRANSAKCJA KARTĄ PŁATNICZĄ', 'Sport'),
  [`15.04.2024`, '15.04.2024', 'PŁATNOŚCI S.A.', 'UL. TESTOWA 8 00-001 KRAKÓW', ACC1, ACC2, '/OPT/X/////P24-A1B-C2D-E3F M00001-00001', '-200,00', 'PLN', "'000000F000000001", 'PRZELEW MOBILE', 'Bez kategorii'].join(';'),
  pekao('15.04.2024', '14.04.2024', 'RIDES  * FOOD PENDING  AMSTERDAM', '', '*********0000002', '-235,00', "'C000000000000004", 'TRANSAKCJA KARTĄ PŁATNICZĄ', 'Restauracje i kawiarnie'),
  pekao('10.04.2024', '10.04.2024', 'PRACODAWCA PRZYKŁADOWY SP. Z O.O.', 'UL. FIRMOWA 1 00-002 WARSZAWA', 'WYNAGRODZENIE 04/2024', '9 250,00', "'C000000000000005", 'PRZELEW PRZYCHODZĄCY', 'Wynagrodzenie')], {final: true});
const pkoCard = (d, v, amt, title, shop, city, when) => [d, v, 'Płatność kartą', amt, 'PLN', `Tytuł: ${title}`, `Lokalizacja: Adres: ${shop} Miasto: ${city} Kraj: POLSKA`,
  `Data wykonania operacji: ${when}`, 'Kwota Cash Back:  0.00', `Oryginalna kwota operacji: ${(-amt).toFixed(2)}`, 'Numer karty: 000000******0001'].join(',');
fx('banks/pko-bp-like.csv', 'PKO BP layout: Windows-1250, CRLF, ",", ISO dates, dot decimals without fixed scale. The header has 6 named + 5 blank columns and extra description fields spill into the blank columns, so the legacy header rule picks a data row (MVP bug). Blank lines between groups; one quoted field contains a comma.', [
  'Data operacji,Data waluty,Typ transakcji,Kwota,Waluta,Opis transakcji,,,,,',
  pkoCard('2026-04-04', '2026-04-03', -43.52, '000000001 00000000000000000000001', 'SKLEP DYSKONT Długa', 'Krakow', '2026-04-03 07:21'),
  '2026-04-04,2026-04-04,Opłata za użytkowanie karty,-10,PLN,"Tytuł: OPŁATA MIESIĘCZNA ZA KARTĘ 000000******0001, 28.02-28.03",,,,,',
  '2026-04-02,2026-03-31,Płatność kartą,-265.13,PLN,Tytuł: 000000002 00000000000000000000002,Lokalizacja: Adres: STACJA PALIW Miasto: KRAKOW Kraj: POLSKA,Data wykonania operacji: 2026-03-31,Kwota Cash Back:  0.00,Oryginalna kwota operacji: 265.13,Numer karty: 000000******0001',
  '',
  pkoCard('2026-03-31', '2026-03-30', -5.65, '000000001 00000000000000000000003', 'SKLEP DYSKONT Długa', 'Krakow', '2026-03-30 16:40'),
  [ '2026-03-13', '2026-03-12', 'Płatność kartą', '-25', 'PLN', 'Tytuł: 000000001 00000000000000000000004', '"Lokalizacja: Adres: BAR SZYBKI 1, Krakow 1 Miasto: Krakow Kraj: POLSKA"', 'Data wykonania operacji: 2026-03-12 13:03', 'Kwota Cash Back:  0.00', 'Oryginalna kwota operacji: 25.00', 'Numer karty: 000000******0001'].join(','),
  '2026-03-10,2026-03-10,Przelew przychodzący,3200,PLN,Tytuł: WYNAGRODZENIE 03/2026,Nadawca: PRACODAWCA PRZYKŁADOWY,Rachunek nadawcy: 00000000000000000000000003,,,',
  '',
  '2026-02-20,2026-02-19,Wypłata z bankomatu,-1600,PLN,Tytuł: BANKOMAT 0000000000000000A0000,Lokalizacja: Adres: Szeroka 1 Miasto: KRAKOW Kraj: POLSKA,Data wykonania operacji: 2026-02-19 17:57,Kwota Cash Back:  0.00,Oryginalna kwota operacji: 1600.00,Numer karty: 000000******0001',
  ''], {enc: 'windows-1250', eol: '\r\n'});
const pad = (s, n) => s + ' '.repeat(Math.max(1, n - s.length));
const mb = (d, desc, cat, amt) => `${d};"${desc}";"Konto 0000 ... 0003";"${cat}";${amt} PLN;;`;
fx('banks/mbank-like.csv', 'mBank layout: UTF-8 with BOM, CRLF, ";". A ~25-line preamble (bank address with leading tabs, client, period, account, per-currency summary). The header "#Data operacji;#Opis operacji;#Rachunek;#Kategoria;#Kwota;" has 6 fields but data rows end in ";;" and have 7, so the legacy rule never picks the header (MVP bug). Amounts carry a " PLN" suffix; descriptions are quoted and padded with long runs of spaces.', [
  'mBank Przykładowy S.A. Bankowość Detaliczna;', '\t\t\tSkrytka Pocztowa 0000;', '\t\t\t00-000 Miasto 2;', '\t\t\twww.example.pl;', '\t\t\tInfolinia: 000 000 000;', '\t\t\t+48 (00) 0 000 000;', '\t\t\t', '',
  '#Klient;', 'JAN PRZYKŁADOWY;', '', 'Lista operacji;', '', '#Za okres:;', '09.04.2025;09.04.2026;', '',
  '#zgodnie z wybranymi filtrami wyszukiwania;', '      #dla rachunków:;', '      Konto - 00000000000000000000000003;', '      ',
  '      #Lista nie jest dokumentem w rozumieniu art. 7 Ustawy Prawo Bankowe, ponieważ operacje można samodzielnie edytować.;', '      ',
  '      #Waluta;#Wpływy;#Wydatki;', 'PLN;584,30;-2 132,22;', '      ',
  '#Data operacji;#Opis operacji;#Rachunek;#Kategoria;#Kwota;',
  mb('2026-04-08', pad('BLIK WYPŁATA ATM WŁASNY  BANKOMAT ALFA', 170) + pad('BLIK WYPŁATA ATM WŁASNY', 84), 'Wypłata gotówki', '-800,00'),
  mb('2026-04-08', pad('SERWIS PRZYKŁADOWY  ZAKUP PRZY UŻYCIU KARTY - MO/TO', 110) + 'transakcja nierozliczona', 'Bez kategorii', '-31,43'),
  mb('2026-04-08', pad('PŁATNIK ZAGRANICZNY 1 EXAMPLE STREET, SINGAPORE SG, 0000000000001 /RFB/0000000000001  SINGAPORE SG', 190) + pad('PRZELEW ZEWNĘTRZNY PRZYCHODZĄCY', 82) + pad('00000000000000000000001001', 28), 'Wpływy - inne', '284,30'),
  mb('2026-04-07', pad('URZĄD PRZYKŁADOWY 000001  ZAKUP PRZY UŻYCIU KARTY - INTERNET', 110) + 'transakcja nierozliczona', 'Płatności - inne', '-82,22'),
  mb('2026-04-07', pad('URZĄD PRZYKŁADOWY 000001  ZAKUP PRZY UŻYCIU KARTY - INTERNET', 110) + 'transakcja nierozliczona', 'Płatności - inne', '-82,22'),
  mb('2026-04-03', pad('FIRMA X  ZAKUP PRZY UŻYCIU KARTY - INTERNET', 110), 'Multimedia, książki i prasa', '-26,44'),
  mb('2026-04-02', pad('FUNDACJA PRZYKŁADOWA, DAROWIZNA NA CELE STATUTOWE', 118) + pad('PRZELEW WEWNĘTRZNY WYCHODZĄCY', 82) + pad('00000000000000000000001002', 28), 'Bez kategorii', '-200,00'),
  mb('2026-04-01', pad('KONTRAHENT  UL.TESTOWA 3                      00-123 MIASTO', 120) + pad('PRZELEW WEWNĘTRZNY PRZYCHODZĄCY', 82) + pad('00000000000000000000001000', 28), 'Wpływy - inne', '300,00'),
  mb('2026-03-20', pad('OPŁATA ZA KARTĘ', 31), 'Opłaty i odsetki', '-10,00'),
  mb('2026-03-16', pad('UBEZPIECZYCIEL.PL', 81) + pad('BLIK ZAKUP E-COMMERCE', 84), 'Ubezpieczenie domu', '-843,43'),
  mb('2026-03-15', pad('FIRMA R', 81) + pad('BLIK ZAKUP E-COMMERCE', 84), 'Zajęcia dodatkowe', '-1 190,00'),
  '', '', ''], {bom: true, eol: '\r\n'});

// A longer export of the same layout: here the 7-field data rows outnumber the preamble lines, so
// the legacy rule takes the first transaction as the header instead of the bank's address line.
const MB_PRE = ['mBank Przykładowy S.A. Bankowość Detaliczna;', '\t\t\tSkrytka Pocztowa 0000;', '\t\t\t00-000 Miasto 2;', '\t\t\twww.example.pl;', '\t\t\tInfolinia: 000 000 000;', '\t\t\t+48 (00) 0 000 000;', '\t\t\t', '',
  '#Klient;', 'JAN PRZYKŁADOWY;', '', 'Lista operacji;', '', '#Za okres:;', '01.01.2026;31.03.2026;', '',
  '#zgodnie z wybranymi filtrami wyszukiwania;', '      #dla rachunków:;', '      Konto - 00000000000000000000000003;', '      ',
  '      #Lista nie jest dokumentem w rozumieniu art. 7 Ustawy Prawo Bankowe, ponieważ operacje można samodzielnie edytować.;', '      ',
  '      #Waluta;#Wpływy;#Wydatki;', 'PLN;9 600,00;-4 204,50;', '      ',
  '#Data operacji;#Opis operacji;#Rachunek;#Kategoria;#Kwota;'];
const mbLong = [];
for (let m = 3; m >= 1; m--){
  const mm = String(m).padStart(2, '0');
  mbLong.push(
    mb(`2026-${mm}-27`, pad('SKLEP PRZYKŁADOWY BETA  ZAKUP PRZY UŻYCIU KARTY', 110), 'Żywność i chemia domowa', `-${120 + m},40`),
    mb(`2026-${mm}-21`, pad('KAWIARNIA GAMMA  ZAKUP PRZY UŻYCIU KARTY', 110), 'Jedzenie poza domem', `-${17 + m},00`),
    mb(`2026-${mm}-18`, pad('APTEKA DELTA', 81) + pad('BLIK ZAKUP E-COMMERCE', 84), 'Zdrowie i uroda', `-${40 + 3 * m},99`),
    mb(`2026-${mm}-15`, pad('OPERATOR PRZYKŁADOWY, FAKTURA 0${m}/2026', 118) + pad('PRZELEW ZEWNĘTRZNY WYCHODZĄCY', 82) + pad('00000000000000000000001003', 28), 'Multimedia, książki i prasa', '-59,00'),
    mb(`2026-${mm}-10`, pad('PRACODAWCA PRZYKŁADOWY  UL.FIRMOWA 1                      00-002 MIASTO', 120) + pad('PRZELEW ZEWNĘTRZNY PRZYCHODZĄCY', 82) + pad('00000000000000000000001004', 28), 'Wynagrodzenie', '3 200,00'),
    mb(`2026-${mm}-05`, pad('STACJA PALIW EPSILON  ZAKUP PRZY UŻYCIU KARTY', 110), 'Paliwo', `-${200 + 11 * m},30`),
    mb(`2026-${mm}-03`, pad('WSPÓLNOTA PRZYKŁADOWA, CZYNSZ 0${m}/2026', 118) + pad('PRZELEW ZEWNĘTRZNY WYCHODZĄCY', 82) + pad('00000000000000000000001005', 28), 'Czynsz i wynajem', '-900,00'),
    mb(`2026-${mm}-01`, pad('OPŁATA ZA KARTĘ', 31), 'Opłaty i odsetki', '-10,00'));
}
fx('banks/mbank-like-long.csv', 'mBank layout, a longer export (24 operations after the same ~25-line preamble): the data rows outnumber the preamble lines, so the legacy rule takes the first transaction as the header (the other form of the MVP bug).', [
  ...MB_PRE, ...mbLong, '', '', ''], {bom: true, eol: '\r\n'});

/* -- overlap set: dedupe across overlapping exports of one account -- */
const OV_H = 'Data operacji;Kontrahent;Tytuł;Kwota;Kategoria';
const ovRows = {
  jan: ['03.01.2025;KAWIARNIA BETA;Kawa;-12,00;Restauracje', '03.01.2025;KAWIARNIA BETA;Kawa;-12,00;Restauracje', '10.01.2025;PRACODAWCA;Wynagrodzenie 01;8 500,00;Wynagrodzenie', '15.01.2025;SKLEP ALFA;Zakupy;-210,40;Zakupy'],
  feb: ['02.02.2025;WSPÓLNOTA;Czynsz 02;-2 850,00;Mieszkanie', '10.02.2025;PRACODAWCA;Wynagrodzenie 02;8 500,00;Wynagrodzenie', '14.02.2025;KWIACIARNIA;Kwiaty;-89,00;Prezenty'],
  mar: ['02.03.2025;WSPÓLNOTA;Czynsz 03;-2 850,00;Mieszkanie', '10.03.2025;PRACODAWCA;Wynagrodzenie 03;8 500,00;Wynagrodzenie', '21.03.2025;SKLEP ALFA;Zakupy;-55,10;Zakupy', '21.03.2025;SKLEP ALFA;Zakupy;-55,10;Zakupy'],
  apr: ['02.04.2025;WSPÓLNOTA;Czynsz 04;-2 850,00;Mieszkanie', '10.04.2025;PRACODAWCA;Wynagrodzenie 04;8 500,00;Wynagrodzenie'],
  may: ['02.05.2025;WSPÓLNOTA;Czynsz 05;-2 850,00;Mieszkanie', '10.05.2025;PRACODAWCA;Wynagrodzenie 05;8 500,00;Wynagrodzenie', '20.05.2025;KAWIARNIA BETA;Kawa;-12,00;Restauracje']
};
fx('overlap/A_jan-mar.csv', 'Overlap set, export A: January–March. Contains two genuine identical coffees on 03.01 and two identical purchases on 21.03.', [OV_H, ...ovRows.mar, ...ovRows.feb, ...ovRows.jan]);
fx('overlap/B_mar-may.csv', 'Overlap set, export B: March–May; March overlaps A (same rows, so they dedupe; the 21.03 pair is kept twice, not four times).', [OV_H, ...ovRows.may, ...ovRows.apr, ...ovRows.mar]);
fx('overlap/C_q1_again.csv', 'Overlap set, export C: the same rows as A plus one genuine identical second purchase (a third 03.01 coffee), so exactly one extra row survives.', [OV_H, ...ovRows.mar, ...ovRows.feb, ovRows.jan[0], ...ovRows.jan]);

/* -- same-name set -- */
fx('same-name/a/export.csv', 'Same-name set: account A export named export.csv. Shares one transaction (11.01 coffee) with b/export.csv, which appears twice in b.', [OV_H,
  '11.01.2025;KAWIARNIA BETA;Kawa;-12,00;Restauracje', '12.01.2025;SKLEP ALFA;Zakupy;-30,00;Zakupy']);
fx('same-name/b/export.csv', 'Same-name set: a different export also named export.csv; legacy bucketed duplicates by file name, so b\'s second 11.01 coffee could be lost.', [OV_H,
  '11.01.2025;KAWIARNIA BETA;Kawa;-12,00;Restauracje', '11.01.2025;KAWIARNIA BETA;Kawa;-12,00;Restauracje', '13.01.2025;APTEKA;Leki;-25,99;Zdrowie']);

/* -- duplicate-versions set -- */
const dv = (d, who, title, amt, ref, typ, cat) => [d, d, who, '', ACC1, '', title, amt, 'PLN', ref, typ, cat].join(';');
fx('dup-versions/v1_jan-feb.csv', 'Duplicate-versions set, older export. Same transactions as v2 for 15.01–28.02 but with the bank\'s original category and operation type.', [PEKAO_H,
  dv('28.02.2025', 'SKLEP ALFA', 'Zakupy luty', '-120,00', "'D000000000000005", 'TRANSAKCJA KARTĄ PŁATNICZĄ', 'Bez kategorii'),
  dv('14.02.2025', 'KWIACIARNIA', 'Kwiaty', '-89,00', "'D000000000000004", 'TRANSAKCJA KARTĄ PŁATNICZĄ', 'Bez kategorii'),
  dv('02.02.2025', 'WSPÓLNOTA', 'Czynsz 02', '-2 850,00', "'D000000000000003", 'PRZELEW MOBILE', 'Mieszkanie'),
  dv('15.01.2025', 'SKLEP ALFA', 'Zakupy styczeń', '-210,40', "'D000000000000002", 'PŁATNOŚĆ BLIK', 'Zakupy'),
  dv('05.01.2025', 'KAWIARNIA BETA', 'Kawa', '-12,00', "'D000000000000001", 'TRANSAKCJA KARTĄ PŁATNICZĄ', 'Restauracje i kawiarnie')]);
fx('dup-versions/v2_jan-mar.csv', 'Duplicate-versions set, newer export: the bank re-labelled categories and changed one operation type; one row without a reference (keyed by date/amount/payee/title).', [PEKAO_H,
  dv('10.03.2025', 'PRACODAWCA', 'Wynagrodzenie 03', '8 500,00', "'D000000000000006", 'PRZELEW PRZYCHODZĄCY', 'Wynagrodzenie'),
  dv('28.02.2025', 'SKLEP ALFA', 'Zakupy luty', '-120,00', "'D000000000000005", 'TRANSAKCJA KARTĄ PŁATNICZĄ', 'Artykuły spożywcze'),
  dv('14.02.2025', 'KWIACIARNIA', 'Kwiaty', '-89,00', "'D000000000000004", 'TRANSAKCJA KARTĄ PŁATNICZĄ', 'Prezenty'),
  dv('02.02.2025', 'WSPÓLNOTA', 'Czynsz 02', '-2 850,00', "'D000000000000003", 'PRZELEW', 'Mieszkanie'),
  dv('15.01.2025', 'SKLEP ALFA', 'Zakupy styczeń', '-210,40', "'D000000000000002", 'PŁATNOŚĆ BLIK', 'Zakupy przez internet'),
  dv('05.01.2025', 'KAWIARNIA BETA', 'Kawa', '-12,00', '', 'TRANSAKCJA KARTĄ PŁATNICZĄ', 'Restauracje i kawiarnie')]);

/* ---------- write / check ---------- */
function bytesOf(f){
  let text = typeof f.lines === 'string' ? f.lines : f.lines.join(f.eol) + (f.final && f.lines.length ? f.eol : '');
  if (f.bom) text = '﻿' + text;
  return encode(text, f.enc);
}
const eolName = f => typeof f.lines === 'string' ? 'mixed' : ({'\n': 'LF', '\r\n': 'CRLF', '\r': 'CR'})[f.eol];
function readme(){
  const sets = [['Edge cases', f => !f.path.includes('/')], ['Bank look-alikes', f => f.path.startsWith('banks/')], ['Overlap set', f => f.path.startsWith('overlap/')],
    ['Same-name set', f => f.path.startsWith('same-name/')], ['Duplicate-versions set', f => f.path.startsWith('dup-versions/')]];
  let s = `# Synthetic fixtures\n\nGenerated by \`tools/legacy-oracle/make-fixtures.mjs\` (\`make fixtures\`). **Do not edit by hand**: change the script and regenerate.\n` +
    `Every value is invented. Account-like numbers use the all-zero form allowlisted by \`tools/check-private.sh\`.\n` +
    `Git treats these files as binary (\`.gitattributes\`), so encodings, BOMs and line endings are preserved byte for byte.\n\n` +
    `${F.length} files.\n`;
  for (const [title, pred] of sets){
    s += `\n## ${title}\n\n| File | Encoding | EOL | BOM | Intent |\n|---|---|---|---|---|\n`;
    for (const f of F.filter(pred)) s += `| \`${f.path}\` | ${f.enc} | ${eolName(f)} | ${f.bom ? 'yes' : 'no'} | ${f.intent.replace(/\|/g, '\\|')} |\n`;
  }
  return s;
}
const want = new Map(F.map(f => [f.path, bytesOf(f)]));
want.set('README.md', Buffer.from(readme(), 'utf8'));

const list = d => existsSync(d) ? readdirSync(d).flatMap(n => { const p = join(d, n); return statSync(p).isDirectory() ? list(p) : [relative(OUT, p)]; }) : [];
if (CHECK){
  const have = new Set(list(OUT)); let bad = 0;
  for (const [p, b] of want){ if (!have.has(p) || !readFileSync(join(OUT, p)).equals(b)){ console.error(`fixtures: ${p} is out of date`); bad = 1; } have.delete(p); }
  for (const p of have){ console.error(`fixtures: ${p} is not produced by the generator`); bad = 1; }
  if (bad){ console.error('Run `make fixtures` and commit the result.'); process.exit(1); }
  console.log(`fixtures: ok (${F.length} files)`);
} else {
  rmSync(OUT, {recursive: true, force: true});
  for (const [p, b] of want){ mkdirSync(dirname(join(OUT, p)), {recursive: true}); writeFileSync(join(OUT, p), b); }
  console.log(`fixtures: wrote ${F.length} files to ${relative(ROOT, OUT)}`);
}
