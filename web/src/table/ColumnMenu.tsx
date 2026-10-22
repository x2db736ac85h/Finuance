// The column menu (PLAN §7.7, legacy openColMenu): sort entries with type-specific labels, the
// condition with its help text, the value checklist (search, All/None on the shown values, counts,
// at most 600) and Hide column / Clear filter. Conditions apply 160 ms after typing stops (legacy),
// Enter applies and closes.

import { useEffect, useRef, useState } from 'react';

import { useDistinct } from '../api/hooks/transactions';
import { fmtInt } from '../lib/format';
import { without } from '../lib/objects';
import { useViews, viewOf } from '../stores/viewStore';
import { Button } from '../ui/Button';
import { Icon } from '../ui/Icon';
import { TextInput } from '../ui/inputs';
import { Popover } from '../ui/Popover';
import { HELP, SORT_LABELS, type ColumnDef } from './columns';
import type { ViewQuery } from './context';

const COND_MS = 160;
const LIMIT = 600;

type Props = { viewId: string; q: ViewQuery; col: ColumnDef; anchor: HTMLElement; onClose: () => void };

const PLACEHOLDER = { number: '>100, -50..50 …', date: '>=01.03.2025, 2025-03 …', text: 'contains…' };

export function ColumnMenu({ viewId, q, col, anchor, onClose }: Props) {
  const patch = useViews((s) => s.patch);
  const filter = useViews((s) => viewOf(s.views, viewId).filters[col.id]);
  const sort = useViews((s) => viewOf(s.views, viewId).sort);
  const [cond, setCond] = useState(filter?.cond ?? '');
  const [search, setSearch] = useState('');
  const [valueSearch, setValueSearch] = useState('');
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  const excluded = filter?.excluded ?? [];
  const values = useDistinct(q.context, q.ctxKey, col.id, valueSearch);

  useEffect(
    () => () => {
      clearTimeout(timer.current);
    },
    [],
  );
  // value search goes to the server after a short pause
  useEffect(() => {
    const t = setTimeout(() => {
      setValueSearch(search.trim());
    }, 200);
    return () => {
      clearTimeout(t);
    };
  }, [search]);

  const setFilter = (next: { cond?: string; excluded?: string[] }) => {
    const cur = viewOf(useViews.getState().views, viewId).filters;
    const f = { cond: next.cond ?? cur[col.id]?.cond ?? '', excluded: next.excluded ?? cur[col.id]?.excluded ?? [] };
    patch(viewId, { filters: { ...cur, [col.id]: f } });
  };
  const applyCond = (v: string, delay: number) => {
    clearTimeout(timer.current);
    if (delay) {
      timer.current = setTimeout(() => {
        setFilter({ cond: v });
      }, delay);
    } else setFilter({ cond: v });
  };
  const setSort = (dir: 'asc' | 'desc') => {
    const on = sort?.column === col.id && sort.dir === dir;
    patch(viewId, { sort: on ? null : { column: col.id, dir } });
  };
  const shown = values.data ?? [];
  const toggle = (value: string, on: boolean) => {
    setFilter({ excluded: on ? excluded.filter((x) => x !== value) : [...excluded, value] });
  };
  const all = (on: boolean) => {
    const vs = new Set(shown.map((v) => v.value));
    setFilter({ excluded: on ? excluded.filter((x) => !vs.has(x)) : [...new Set([...excluded, ...vs])] });
  };
  const [ascLabel, descLabel] = SORT_LABELS[col.type];

  return (
    <Popover open anchor={anchor} onClose={onClose} label={`${col.label} column`} width={310}>
      <div className="pop-h">
        <span className={`tt ${col.type}`}>
          {col.type === 'number' ? '#' : col.type === 'date' ? <Icon name="cal" /> : 'Aa'}
        </span>
        <b>{col.label}</b>
        <span className="muted">
          {col.type}
          {values.data && ` · ${shown.length >= LIMIT ? `${fmtInt(LIMIT)}+` : fmtInt(shown.length)} values`}
        </span>
      </div>
      <div className="pop-s">
        <button
          type="button"
          className={sort?.column === col.id && sort.dir === 'asc' ? 'mi on' : 'mi'}
          onClick={() => {
            setSort('asc');
          }}
        >
          <Icon name="up" />
          Sort ascending<span className="hint">{ascLabel}</span>
        </button>
        <button
          type="button"
          className={sort?.column === col.id && sort.dir === 'desc' ? 'mi on' : 'mi'}
          onClick={() => {
            setSort('desc');
          }}
        >
          <Icon name="down" />
          Sort descending<span className="hint">{descLabel}</span>
        </button>
      </div>
      <div className="pop-s">
        <div className="lbl">Condition</div>
        <TextInput
          mono
          aria-label={`Condition for ${col.label}`}
          placeholder={PLACEHOLDER[col.type]}
          value={cond}
          spellCheck={false}
          autoComplete="off"
          autoFocus
          onFocus={(e) => {
            e.currentTarget.select();
          }}
          onChange={(e) => {
            setCond(e.target.value);
            applyCond(e.target.value, COND_MS);
          }}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault();
              applyCond(cond, 0);
              onClose();
            }
          }}
        />
        <div className="hint">
          {HELP[col.type].map((g, i) => (
            <span key={i}>
              {i > 0 && ' · '}
              {g.codes.map((c, k) => (
                <span key={c}>
                  {k > 0 && ' '}
                  <code>{c}</code>
                </span>
              ))}
              {g.text && `${g.codes.length ? ' ' : ''}${g.text}`}
            </span>
          ))}
        </div>
      </div>
      <div className="pop-s grow">
        <div className="lbl">
          Values
          <span className="spacer" />
          <button
            type="button"
            className="link"
            onClick={() => {
              all(true);
            }}
          >
            All
          </button>
          <button
            type="button"
            className="link"
            onClick={() => {
              all(false);
            }}
          >
            None
          </button>
        </div>
        <TextInput
          placeholder="Search values…"
          aria-label="Search values"
          autoComplete="off"
          spellCheck={false}
          value={search}
          onChange={(e) => {
            setSearch(e.target.value);
          }}
        />
        <div className="vlist" role="group" aria-label={`Values of ${col.label}`}>
          {shown.map((v) => (
            <label key={v.value} className="vi">
              <input
                type="checkbox"
                checked={!excluded.includes(v.value)}
                onChange={(e) => {
                  toggle(v.value, e.target.checked);
                }}
              />
              <span className="vt" title={v.value || undefined}>
                {v.value === '' ? <span className="nil">(empty)</span> : v.value}
              </span>
              <span className="vc">{fmtInt(v.count)}</span>
            </label>
          ))}
          {values.data && !shown.length && <div className="vmore">No matching values</div>}
          {shown.length >= LIMIT && (
            <div className="vmore">Only the {fmtInt(LIMIT)} most frequent — refine the search</div>
          )}
        </div>
      </div>
      <div className="pop-f">
        <Button
          variant="ghost"
          icon="eyeoff"
          onClick={() => {
            const v = viewOf(useViews.getState().views, viewId);
            patch(viewId, { hidden: [...v.hidden, col.id] });
            onClose();
          }}
        >
          Hide column
        </Button>
        <Button
          onClick={() => {
            clearTimeout(timer.current);
            setCond('');
            patch(viewId, { filters: without(viewOf(useViews.getState().views, viewId).filters, col.id) });
          }}
        >
          Clear filter
        </Button>
      </div>
    </Popover>
  );
}
