// Appearance controls (legacy appearanceHtml): the colour theme (Phase 19), System/Dark/Light and
// the interface size, on Settings → General (every theme as a card) and in the quick settings
// (`QuickAppearance`, Phase 26: the current theme with a dark-mode switch on its line, and a plain
// slider). The size is a slider around Auto (Phase 20, 20.15).

import { useEffect, useId, useState, useSyncExternalStore, type ReactNode } from 'react';

import { useMeta } from '../api/hooks/me';
import { fmtPct } from '../lib/format';
import { SIZE_STEPS, sliderStep, uiScale, type SizeStep } from '../styles/scale';
import { LIGHT_QUERY, resolveLook, resolveTheme, type ThemePref } from '../styles/theme';
import { BUILTIN_THEMES, type ThemeTokens } from '../styles/themes';
import { useUi } from '../stores/uiStore';
import { Icon } from '../ui/Icon';

const THEMES: [ThemePref, string][] = [
  ['system', 'System'],
  ['dark', 'Dark'],
  ['light', 'Light'],
];

/** The scale in effect, refreshed on resize (auto follows the browser zoom). */
function useCurrentScale(): number {
  const pref = useUi((s) => s.uiScalePref);
  const step = useUi((s) => s.uiStep);
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
  }, [pref, step]);
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

/**
 * The built-in themes and the ones an administrator approved; each shows both variants. Compact (the
 * quick settings): one button with the current theme, which unfolds the others as small cards.
 */
export function ThemePicker({ compact = false, aside }: { compact?: boolean; aside?: ReactNode }) {
  const labelId = useId();
  const [open, setOpen] = useState(false);
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
  const cards = (
    <div className={compact ? 'theme-cards compact' : 'theme-cards'} role="radiogroup" aria-labelledby={labelId}>
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
            setOpen(false);
          }}
        >
          <Swatch tokens={t.tokens} />
          <span className="theme-name">{t.name}</span>
        </button>
      ))}
    </div>
  );
  if (!compact)
    return (
      <div className="st-col">
        <span id={labelId}>Colour theme</span>
        {cards}
      </div>
    );
  const shown = choices.find((t) => t.id === current) ?? choices[0];
  return (
    <div className="st-col">
      <span id={labelId} className="sr-only">
        Colour theme
      </span>
      <div className="qs-look">
        <button
          type="button"
          className="theme-current"
          aria-expanded={open}
          aria-describedby={labelId}
          onClick={() => {
            setOpen(!open);
          }}
        >
          {shown && <Swatch tokens={shown.tokens} />}
          <span className="theme-name">{shown?.name}</span>
          <Icon name={open ? 'chevup' : 'chev'} />
        </button>
        {aside}
      </div>
      {open && cards}
    </div>
  );
}

/** Words for a slider position. */
function sizeWord(fixed: boolean, step: SizeStep): string {
  if (fixed) return 'Fixed';
  if (step === 0) return 'Auto';
  return step < 0 ? 'Smaller' : 'Larger';
}

/**
 * The interface size: a slider with Auto in the middle (the same size at any browser zoom) and two
 * steps of 10 % to each side. A fixed size from an older document shows at its nearest step until
 * the slider is moved.
 */
export function SizeSlider({ minimal = false }: { minimal?: boolean }) {
  const pref = useUi((s) => s.uiScalePref);
  const step = useUi((s) => s.uiStep);
  const now = useCurrentScale();
  const id = useId();
  const at = sliderStep(pref, step, window.devicePixelRatio);
  const shown = `${sizeWord(pref !== 'auto', at)} · ${fmtPct(now)}`;
  const range = (
    <input
      id={id}
      type="range"
      min={SIZE_STEPS[0]}
      max={SIZE_STEPS[SIZE_STEPS.length - 1]}
      step={1}
      value={at}
      aria-valuetext={shown}
      onChange={(e) => {
        useUi.getState().setUiStep(Number(e.target.value) as SizeStep);
      }}
    />
  );
  // the quick settings: one line, a thin track, the word for the position at its end
  if (minimal)
    return (
      <div className="size-slider minimal">
        <label htmlFor={id}>Interface size</label>
        {range}
        <span className="hint">{sizeWord(pref !== 'auto', at)}</span>
      </div>
    );
  return (
    <div className="st-col size-slider">
      <span className="size-h">
        <label htmlFor={id}>Interface size</label>
        <span className="hint">{shown}</span>
      </span>
      {range}
      <span className="size-ticks" aria-hidden="true">
        <span>Smaller</span>
        <span>Auto</span>
        <span>Larger</span>
      </span>
    </div>
  );
}

function watchLight(onChange: () => void): () => void {
  const mq = window.matchMedia(LIGHT_QUERY);
  mq.addEventListener('change', onChange);
  return () => {
    mq.removeEventListener('change', onChange);
  };
}

/**
 * Dark or light as one switch (the quick settings). It shows the mode in effect: with "System"
 * chosen that is the computer's, and the app keeps following it until the switch is used.
 * "System" itself is chosen on Settings → General.
 */
export function DarkSwitch() {
  const pref = useUi((s) => s.themePref);
  const prefersLight = useSyncExternalStore(watchLight, () => window.matchMedia(LIGHT_QUERY).matches);
  const dark = resolveTheme(pref, prefersLight) === 'dark';
  return (
    <button
      type="button"
      role="switch"
      aria-checked={dark}
      aria-label="Dark mode"
      title={dark ? 'Dark mode is on' : 'Dark mode is off'}
      className={dark ? 'mode-switch on' : 'mode-switch'}
      onClick={() => {
        useUi.getState().setTheme(dark ? 'light' : 'dark');
      }}
    >
      <Icon name="sun" />
      <Icon name="moon" />
      <span className="mode-switch-thumb" aria-hidden="true" />
    </button>
  );
}

/** The quick settings' two lines: the theme with the dark-mode switch, and the size. */
export function QuickAppearance() {
  return (
    <>
      <ThemePicker compact aside={<DarkSwitch />} />
      <SizeSlider minimal />
    </>
  );
}

/** Settings → General: every theme, System/Dark/Light and the labelled slider. */
export function AppearanceControls() {
  const theme = useUi((s) => s.themePref);
  const { setTheme } = useUi.getState();
  const modeId = useId();
  return (
    <>
      <ThemePicker />
      <div className="st-line">
        <span id={modeId}>Mode</span>
        <span className="seg seg-l" role="group" aria-labelledby={modeId}>
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
      <SizeSlider />
    </>
  );
}
