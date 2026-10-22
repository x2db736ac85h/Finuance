// Appearance controls (legacy appearanceHtml): theme System/Dark/Light and the interface size,
// shown inline in the settings popover and on Settings → Appearance.

import { useEffect, useState } from 'react';

import { fmtPct } from '../lib/format';
import { MANUAL_SCALES, uiScale } from '../styles/scale';
import type { ThemePref } from '../styles/theme';
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
      <div className="st-line">
        <span id="theme-label">Theme</span>
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
