// Tooltip content of the charts (legacy tipHtml): a title, rows of [colour, label, value] and a
// muted hint. Tooltips only repeat what the table twin shows (PLAN §7.1).

export type TipRow = [color: string, label: string, value: string];

export function TipContent({
  title,
  rows,
  hint,
}: {
  title: string;
  rows: readonly TipRow[];
  hint?: string | undefined;
}) {
  return (
    <>
      <div className="tt-h">{title}</div>
      {rows.map(([col, k, v]) => (
        <div className="r" key={k}>
          {col && <i className="sw" style={{ background: col }} />}
          <span className="k">{k}</span>
          <span className="v">{v}</span>
        </div>
      ))}
      {hint && (
        <div className="hint">
          <span className="muted">{hint}</span>
        </div>
      )}
    </>
  );
}
