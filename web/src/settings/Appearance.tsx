// Appearance controls (legacy appearanceHtml): the colour theme (Phase 19), System/Dark/Light and
// the interface size, shown inline in the settings popover and on Settings → Appearance.

import { useEffect, useState } from 'react';

import { useMeta } from '../api/hooks/me';
import { fmtPct } from '../lib/format';
import { MANUAL_SCALES, uiScale } from '../styles/scale';
import { resolveLook, type ThemePref } from '../styles/theme';
import { BUILTIN_THEMES, type ThemeTokens } from '../styles/themes';
import { useUi } from '../stores/uiStore';
import { Select, type Option } from '../ui/inputs';

const THEMES: [ThemePref, string][] = [
  ['system', 'System'],
  ['dark', 'Dark'],
  ['light', 'Light'],
];

/** The scale in effect, refreshed on resize (auto follows the browser zoom). */
function useCurrentScale(): number {
  const pref = useUi((s) => s.uiScalePref);
  const [scale, setScale] = useState(uiScale);
  useEffect(() => {
    const update = () => {
      setScale(uiScale());
    };
    // after the appearance effect has applied the new preference
    const t = setTimeout(update, 0);
    window.addEventListener('resize', update);
    return () => {
      clearTimeout(t);
      window.removeEventListener('resize', update);
    };
  }, [pref]);
  return scale;
}

type ThemeChoice = { id: string; name: string; description: string; tokens: ThemeTokens };

/** The colours a theme card shows: page, panel, text, accent and the solid button. */
function Swatch({ tokens }: { tokens: ThemeTokens }) {
  return (
    <span className="theme-sw" aria-hidden="true">
      {(['dark', 'light'] as const).map((m) => (
        <span key={m} className="theme-sw-half" style={{ background: tokens[m]['bg'] }}>
          <span className="theme-sw-panel" style={{ background: tokens[m]['panel'], color: tokens[m]['text'] }}>
            Aa
          </span>
          <span className="theme-sw-dot" style={{ background: tokens[m]['accent'] }} />
          <span className="theme-sw-dot" style={{ background: tokens[m]['accent-solid'] }} />
        </span>
      ))}
    </span>
  );
}

/** The built-in themes and the ones an administrator approved; each shows both variants. */
export function ThemePicker() {
  const themeId = useUi((s) => s.themeId);
  const { setThemeId } = useUi.getState();
  const meta = useMeta().data;
  const choices: ThemeChoice[] = [
    ...BUILTIN_THEMES,
    ...(meta?.themes ?? []).map((t) => ({
      id: t.id,
      name: t.name,
      description: 'Approved by an administrator',
      tokens: t.tokens,
    })),
  ];
  const current = resolveLook(themeId, meta?.themes ?? [], meta?.defaults.themeId).id;
  return (
    <div className="st-col">
      <span id="colour-theme-label">Colour theme</span>
      <div className="theme-cards" role="radiogroup" aria-labelledby="colour-theme-label">
        {choices.map((t) => (
          <button
            key={t.id}
            type="button"
            role="radio"
            aria-checked={current === t.id}
            className={current === t.id ? 'theme-card on' : 'theme-card'}
            title={t.description}
            onClick={() => {
              setThemeId(t.id);
            }}
          >
            <Swatch tokens={t.tokens} />
            <span className="theme-name">{t.name}</span>
          </button>
        ))}
      </div>
    </div>
  );
}

export function AppearanceControls() {
  const theme = useUi((s) => s.themePref);
  const pref = useUi((s) => s.uiScalePref);
  const { setTheme, setUiScale } = useUi.getState();
  const now = useCurrentScale();
  const sizes: Option<string>[] = [
    { value: 'auto', label: `Auto — keeps the same size at any browser zoom (now ${fmtPct(now)})` },
    ...MANUAL_SCALES.map((s) => ({ value: String(s), label: fmtPct(s) })),
  ];
  return (
    <>
      <ThemePicker />
      <div className="st-line">
        <span id="theme-label">Mode</span>
        <span className="seg seg-l" role="group" aria-labelledby="theme-label">
          {THEMES.map(([k, label]) => (
            <button
              key={k}
              type="button"
              className={theme === k ? 'on' : undefined}
              aria-pressed={theme === k}
              onClick={() => {
                setTheme(k);
              }}
            >
              {label}
            </button>
          ))}
        </span>
      </div>
      <label className="st-col">
        Interface size
        <Select
          value={String(pref)}
          options={sizes}
          onChange={(v) => {
            setUiScale(v === 'auto' ? 'auto' : Number(v));
          }}
        />
      </label>
    </>
  );
}
