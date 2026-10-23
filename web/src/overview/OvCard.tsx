// One overview card (legacy renderOverview's section.card.ov-card): the grid places it by its key
// class; the body has an explicit height (app.css), so the chart measures a stable size.

import type { ChartDef } from '../charts/types';
import { useChartEnv } from '../insights/useChartEnv';
import { drawable, useSize } from '../insights/useSize';
import type { ViewQuery } from '../table/context';
import { IconButton } from '../ui/IconButton';

type Props = { def: ChartDef; viewId: string; q: ViewQuery; onExpand: () => void };

export function OvCard({ def, viewId, q, onExpand }: Props) {
  const [body, size] = useSize();
  const env = useChartEnv(viewId, size.w, size.h, false);
  const parts = def.use(q, env);
  return (
    <section className={`card ov-card ${def.key}`} aria-label={def.title} aria-busy={parts.loading}>
      <div className="card-h">
        <span>{def.title}</span>
        <span className="sub">{parts.sub}</span>
        <span className="spacer" />
        <span className="ch-x">{parts.legend}</span>
        {def.expandable !== false && (
          <IconButton
            icon="expand"
            size="sm"
            title="Open larger — chart, table and range settings"
            onClick={onExpand}
          />
        )}
      </div>
      <div className="card-b" ref={body}>
        {drawable(size) && parts.body}
      </div>
    </section>
  );
}
