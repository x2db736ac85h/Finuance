// The legacy svgTag: charts draw in logical units (w × h) and the viewBox scales them to the
// interface size, so text, strokes and padding grow with it. SVG text stays in px (logical).

import type { ReactNode, Ref } from 'react';

import { uiScale } from '../../styles/scale';

type Props = { w: number; h: number; label: string; children: ReactNode; svgRef?: Ref<SVGSVGElement> };

export function Svg({ w, h, label, children, svgRef }: Props) {
  const s = uiScale();
  return (
    <svg
      ref={svgRef}
      width={(w * s).toFixed(1)}
      height={(h * s).toFixed(1)}
      viewBox={`0 0 ${w.toFixed(1)} ${h.toFixed(1)}`}
      role="img"
      aria-label={label}
    >
      {children}
    </svg>
  );
}
