// One message of the chat (legacy renderChat and stepHtml): a question, or an answer with its
// reasoning, its lookups (exactly what went back to the model, with Show in table) and its meta line.

import { fmtInt, plural } from '../lib/format';
import { Icon } from '../ui/Icon';
import type { Step } from '../api/hooks/assistant';
import type { AiTurn, UiTurn } from './chatStore';
import { fixFor } from './errors';
import { Markdown } from './md';

const NICE: Record<string, string> = {
  search_transactions: 'Searched transactions',
  aggregate_transactions: 'Summarised transactions',
  find_values: 'Looked up names',
  month_overview: 'Read the month overview',
  recurring_payments: 'Looked for recurring payments',
};

function pretty(json: string): string {
  try {
    return JSON.stringify(JSON.parse(json), null, 1);
  } catch {
    return json;
  }
}

export function StepView({ step, onShow }: { step: Step; onShow?: (s: Step) => void }) {
  return (
    <details className="step">
      <summary>
        <Icon name="search" />
        <span className="st-t">
          <b>{NICE[step.tool] ?? step.tool}</b> · {step.summary}
        </span>
        {step.toolFilter && onShow && (
          <button
            type="button"
            className="link"
            title="Show the matching rows in the table"
            onClick={(e) => {
              e.preventDefault();
              onShow(step);
            }}
          >
            Show in table
          </button>
        )}
      </summary>
      <div className="step-b">
        <div className="lbl">Sent to the model</div>
        <pre>{pretty(step.result) + (step.truncated ? '\n…' : '')}</pre>
      </div>
    </details>
  );
}

/** The legacy meta line: "2.4 s · ~1,830 tokens sent · 1 lookup · plan mode". */
export function metaLine(t: AiTurn): string {
  const m = t.meta;
  if (!m) return '';
  const parts = [
    `${(m.durationMs / 1000).toFixed(1)} s`,
    `~${fmtInt(m.approxTokensSent)} tokens sent`,
    plural(m.lookups, 'lookup'),
  ];
  if (m.mode === 'plan') parts.push('plan mode');
  if (m.stopped) parts.push('stopped');
  parts.push(m.model);
  return parts.join(' · ');
}

export function TurnView({
  turn,
  last,
  onShow,
}: {
  turn: UiTurn;
  last: boolean;
  onShow: (s: Step, t: AiTurn) => void;
}) {
  if (turn.role === 'user') {
    return (
      <div className="msg user">
        <div className="bub">{turn.content}</div>
      </div>
    );
  }
  const thinking = turn.reasoning.trim();
  const fix = turn.error && fixFor(turn.error.code);
  return (
    <div className="msg ai">
      {thinking && (
        <details className="think">
          <summary>Thinking{turn.pending && last && !turn.content ? '…' : ''}</summary>
          <div>{thinking}</div>
        </details>
      )}
      {turn.steps.map((s, i) => (
        <StepView
          key={i}
          step={s}
          onShow={(st) => {
            onShow(st, turn);
          }}
        />
      ))}
      <div className="bub md">
        {turn.content ? (
          <Markdown text={turn.content} />
        ) : turn.pending ? (
          <span className="typing" aria-label="Writing">
            <i />
            <i />
            <i />
          </span>
        ) : null}
      </div>
      {turn.error && (
        <div className="conn-err" role="alert">
          {turn.error.message}
          {fix && turn.error.message !== fix ? ` ${fix}` : ''}
        </div>
      )}
      {turn.meta && <div className="meta">{metaLine(turn)}</div>}
    </div>
  );
}
