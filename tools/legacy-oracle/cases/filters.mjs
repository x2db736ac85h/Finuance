// (legacyColumn, cond) cases evaluated through compileCond on the legacy demo dataset.
const D = 'Data księgowania', V = 'Data waluty', A = 'Kwota operacji', P = 'Nadawca / Odbiorca', C = 'Kategoria', T = 'Typ operacji', R = 'Numer referencyjny', TI = 'Tytułem';

export default [
  // number column: operators, ranges, negation, locale-tolerant numbers, fallbacks
  [A, '>0'], [A, '>=0'], [A, '<0'], [A, '<=0'], [A, '=0'], [A, '>100'], [A, '>=100'], [A, '<-100'], [A, '<=-2850'], [A, '=-2850'],
  [A, '=-2 850,00'], [A, '=-2850,00'], [A, '-100..-50'], [A, '-50..-100'], [A, '-50 .. -100'], [A, '9000..10000'], [A, '!>100'],
  [A, '!-100..-50'], [A, '>1 000'], [A, '<-1.000,00'], [A, '>abc'], [A, '79'], [A, '-79,99'], [A, '='], [A, '!='], [A, '!'],
  [A, '>'], [A, '..'], [A, '1..'], [A, '  >  100  '],
  // date column: looseDate intervals
  [D, '2024'], [D, '=2024'], [D, '>2024'], [D, '>=2025'], [D, '<2024-06'], [D, '<=2024-06'], [D, '2025-03'], [D, '=2025-03'],
  [D, '03.2025'], [D, '01.2025..03.2025'], [D, '2024-12..2024-01'], [D, '>2025-03-10'], [D, '>=2025-03-10'], [D, '2025-03-10'],
  [D, '10.03.2025'], [D, '!2024'], [D, '>garbage'], [D, '2025-03-10..2025-03-12'], [D, '='], [D, '!='], [V, '2024-01-01'],
  [V, '<2024-01-02'],
  // text columns: contains, exact, empty, negation, accents
  [P, 'kino'], [P, 'KINO'], [P, '!kino'], [P, '=KINO POD GWIAZDAMI   WARSZAWA'], [P, '=kino pod gwiazdami   warszawa'],
  [P, '=KINO POD GWIAZDAMI WARSZAWA'], [P, 'wspolnota'], [P, 'WSPÓLNOTA'], [P, 'lipa'], [P, 'lipą'], [P, 'sloneczna'],
  [P, ''], [P, '   '], [P, '='], [P, '!='], [P, '!'], [P, '>a'], [P, 'a..b'],
  [C, 'Rachunki'], [C, '=Rachunki'], [C, '=rachunki'], [C, '!Rachunki'], [C, 'zakupy'], [C, 'ogrod'], [C, '=Bez kategorii'],
  [T, 'blik'], [T, '=PRZELEW MOBILE'], [T, 'karta'], [T, 'kartą'], [R, 'demo'], [R, '!demo'], [TI, 'demo 1']
];
