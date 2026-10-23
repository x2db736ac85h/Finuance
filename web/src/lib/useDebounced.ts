// A value that follows its input after `ms` of quiet (the rule window's live preview). Values are
// compared by their JSON, so a new object with the same content doesn't restart the wait.

import { useEffect, useState } from 'react';

export function useDebounced<T>(value: T, ms: number): T {
  const key = JSON.stringify(value);
  const [out, setOut] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => {
      setOut(JSON.parse(key) as T);
    }, ms);
    return () => {
      clearTimeout(t);
    };
  }, [key, ms]);
  return out;
}
