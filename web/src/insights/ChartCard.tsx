// One card of the insights pane (legacy renderVizCards): title, subtitle, legend/controls and the
// expand button over a body the chart fills at its measured logical size.

import type { ChartDef } from '../charts/types';
import type { ViewQuery } from '../table/context';
import { IconButton } from '../ui/IconButton';
import { useChartEnv } from './useChartEnv';
import { drawable, useSize } from './useSize';

type Props = { def: ChartDef; viewId: string; q: ViewQuery; ready: boolean; onExpand: () => void };

export function ChartCard({ def, viewId, q, ready, onExpand }: Props) {
  const [body, size] = useSize();
  const env = useChartEnv(viewId, size.w, size.h, false, ready);
  const parts = def.use(q, env);
  return (
    <section className={`card c-${def.key}`} aria-label={def.title} aria-busy={parts.loading}>
      <div className="card-h">
        <span>{def.title}</span>
        <span className="sub">{parts.sub}</span>
        <span className="spacer" />
        <span className="ch-x">{parts.legend}</span>
        <IconButton icon="expand" size="sm" title="Open larger — chart, table and range settings" onClick={onExpand} />
      </div>
      <div className="card-b" ref={body}>
        {drawable(size) && parts.body}
      </div>
    </section>
  );
}
