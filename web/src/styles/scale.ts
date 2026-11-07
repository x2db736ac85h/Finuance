// Interface scaling (PLAN §7.5, port of legacy browserZoom/calcUI/applyUI). Every rem in the CSS
// follows --ui-scale; JS-side pixel sizes multiply by the current scale.

/** The auto mode aims for the size the app has at 112 % in a browser at 100 % zoom. */
export const UI_TARGET = 1.12;
export const MIN_SCALE = 0.85;
export const MAX_SCALE = 1.25;
/** Fixed sizes: what older preference documents and legacy backups may hold (still applied). */
export const MANUAL_SCALES = [0.9, 1, 1.1, 1.2, 1.3] as const;
/** The size slider (Phase 20, 20.15): steps around the auto size, each 10 % of it, Auto in the middle. */
export const SIZE_STEPS = [-2, -1, 0, 1, 2] as const;
export type SizeStep = (typeof SIZE_STEPS)[number];
const STEP = 0.1;
const MIN_STEPPED = 0.75;
const MAX_STEPPED = 1.45;

export type UiScalePref = 'auto' | number;

/**
 * Estimates the browser zoom from devicePixelRatio: the base display is taken as 1× or 2×,
 * whichever gives a zoom closest to 1 (legacy browserZoom; tuned for macOS).
 */
export function browserZoom(devicePixelRatio: number): number {
  const d = devicePixelRatio || 1;
  let best = 1;
  let bestDistance = Infinity;
  for (const base of [1, 2]) {
    const z = d / base;
    const distance = Math.abs(Math.log(z));
    if (distance < bestDistance) {
      bestDistance = distance;
      best = z;
    }
  }
  return best;
}

/**
 * The scale for a preference: a fixed size as given; auto from the zoom, then moved by the slider's
 * step (rounded to 0.01).
 */
export function calcScale(pref: UiScalePref, devicePixelRatio: number, step: SizeStep = 0): number {
  if (pref !== 'auto') return pref;
  let s = Math.max(MIN_SCALE, Math.min(MAX_SCALE, UI_TARGET / browserZoom(devicePixelRatio)));
  if (step !== 0) s = Math.max(MIN_STEPPED, Math.min(MAX_STEPPED, s * (1 + STEP * step)));
  return Math.round(s * 100) / 100;
}

/** The slider position that shows a preference: its step, or for a fixed size the nearest step. */
export function sliderStep(pref: UiScalePref, step: SizeStep, devicePixelRatio: number): SizeStep {
  if (pref === 'auto') return step;
  const auto = calcScale('auto', devicePixelRatio);
  let best: SizeStep = 0;
  for (const k of SIZE_STEPS) {
    if (
      Math.abs(calcScale('auto', devicePixelRatio, k) - pref) <
      Math.abs(calcScale('auto', devicePixelRatio, best) - pref)
    )
      best = k;
  }
  return auto === pref ? 0 : best;
}

let current = 1;

/** The scale in effect (for pixel sizes computed in JS). */
export function uiScale(): number {
  return current;
}

/** Sets --ui-scale on <html>. Returns the applied scale. */
export function applyScale(
  pref: UiScalePref,
  root: HTMLElement = document.documentElement,
  step: SizeStep = 0,
): number {
  current = calcScale(pref, window.devicePixelRatio, step);
  root.style.setProperty('--ui-scale', String(current));
  return current;
}
