/** A copy of `o` without the key `k`. */
export function without<K extends string, V>(o: Partial<Record<K, V>>, k: K): Partial<Record<K, V>> {
  const out: Partial<Record<K, V>> = {};
  for (const key of Object.keys(o) as K[]) {
    if (key !== k) out[key] = o[key];
  }
  return out;
}
