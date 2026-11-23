// What the load costs (PLAN 25.16, ADR 0044): the lab's figures from its Prometheus, read by the
// simulator every five seconds, as small charts of the last fifteen minutes.
import { spark } from '../../src/charts/lib/geom';
import type { Client, Schemas } from './api';
import { useResources } from './hooks';

type Point = Schemas['Point'];

const last = (ps: Point[]) => ps.at(-1)?.v;

function Spark({ points }: { points: Point[] }) {
  const s = spark(points.map((p) => p.v));
  if (!s) return <svg className="sim-spark" viewBox="0 0 110 30" aria-hidden="true" />;
  return (
    <svg className="sim-spark" viewBox="0 0 110 30" aria-hidden="true">
      <path d={s.d} />
      <circle cx={s.last[0]} cy={s.last[1]} r={2} />
    </svg>
  );
}

const SERIES: Record<Schemas['ResourceSeries']['name'], { label: string; show: (v: number) => string }> = {
  actions: { label: "The animals' actions a second", show: (v) => v.toFixed(1) },
  requests: { label: 'API requests a second', show: (v) => v.toFixed(1) },
  p95: { label: 'API p95', show: (v) => `${String(Math.round(v * 1000))} ms` },
  serverErrors: { label: 'Server errors a second', show: (v) => v.toFixed(2) },
};

const mib = (v: number) => `${String(Math.round(v / 2 ** 20))} MiB`;

export function Resources({ client }: { client: Client }) {
  const res = useResources(client);
  const data = res.data;
  return (
    <section aria-label="What the load costs" className="sim-resources">
      <h2>What the load costs</h2>
      {!data ? (
        <p className="hint">{res.error ? 'The simulator does not answer.' : 'Loading…'}</p>
      ) : !data.available ? (
        <p className="hint">No figures: {data.error ?? "the lab's Prometheus does not answer"}.</p>
      ) : (
        <>
          <div className="sim-tiles">
            {data.series.map((s) => {
              const v = last(s.points);
              return (
                <div key={s.name} className="sim-tile" role="group" aria-label={SERIES[s.name].label}>
                  <span className="hint">{SERIES[s.name].label}</span>
                  <b>{v === undefined ? '–' : SERIES[s.name].show(v)}</b>
                  <Spark points={s.points} />
                </div>
              );
            })}
          </div>
          <table className="sim-containers">
            <caption className="hint">Each container, the last 15 minutes</caption>
            <thead>
              <tr>
                <th>Container</th>
                <th>CPU, cores</th>
                <th />
                <th>Memory</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {data.containers.map((c) => (
                <tr key={c.name}>
                  <td className="mono">{c.name}</td>
                  <td>{last(c.cpu)?.toFixed(2) ?? '–'}</td>
                  <td>
                    <Spark points={c.cpu} />
                  </td>
                  <td>{mib(last(c.memoryBytes) ?? 0)}</td>
                  <td>
                    <Spark points={c.memoryBytes} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </>
      )}
    </section>
  );
}
