// The logical size of an element (legacy drawChart): pixels / interface scale, re-measured by a
// ResizeObserver. Charts draw nothing below 40 px (a collapsed or hidden card). The ref is a
// callback, so measuring starts whenever the element appears (the modal mounts in a portal).

import { useLayoutEffect, useState } from 'react';

import { uiScale } from '../styles/scale';

export type Size = { w: number; h: number; px: { w: number; h: number } };

const ZERO: Size = { w: 0, h: 0, px: { w: 0, h: 0 } };

export function useSize(): [ref: (el: HTMLElement | null) => void, size: Size] {
  const [el, setEl] = useState<HTMLElement | null>(null);
  const [size, setSize] = useState<Size>(ZERO);
  useLayoutEffect(() => {
    if (!el) return;
    const measure = () => {
      const pw = el.clientWidth;
      const ph = el.clientHeight;
      const s = uiScale();
      setSize((o) =>
        o.px.w === pw && o.px.h === ph && o.w === pw / s ? o : { w: pw / s, h: ph / s, px: { w: pw, h: ph } },
      );
    };
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => {
      ro.disconnect();
    };
  }, [el]);
  return [setEl, size];
}

/** Whether a measured area is big enough to draw in (legacy: 40 px). */
export const drawable = (s: Size) => s.px.w >= 40 && s.px.h >= 40;
