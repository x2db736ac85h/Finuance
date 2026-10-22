// Search normalisation (legacy norm): lower case, accents removed, ł → l, so "platnosc" finds
// "PŁATNOŚĆ". The server normalises the same way for search.

export function norm(s: string): string {
  return s.toLowerCase().normalize('NFD').replace(/[̀-ͯ]/g, '').replace(/ł/g, 'l');
}

/** Search tokens: every whitespace-separated part must occur (legacy compute()). */
export function searchTokens(search: string): string[] {
  return norm(search).split(/\s+/).filter(Boolean);
}
