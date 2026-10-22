// Interface scaling (PLAN §7.5, port of legacy browserZoom/calcUI/applyUI). Every rem in the CSS
// follows --ui-scale; JS-side pixel sizes multiply by the current scale.

/** The auto mode aims for the size the app has at 112 % in a browser at 100 % zoom. */
export const UI_TARGET = 1.12;
export const MIN_SCALE = 0.85;
export const MAX_SCALE = 1.25;
/** Manual sizes offered in Settings → Appearance. */
export const MANUAL_SCALES = [0.9, 1, 1.1, 1.2, 1.3] as const;

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

/** The scale for a preference: manual values as given, auto from the zoom (rounded to 0.01). */
export function calcScale(pref: UiScalePref, devicePixelRatio: number): number {
  if (pref !== 'auto') return pref;
  const s = Math.max(MIN_SCALE, Math.min(MAX_SCALE, UI_TARGET / browserZoom(devicePixelRatio)));
  return Math.round(s * 100) / 100;
}

let current = 1;

/** The scale in effect (for pixel sizes computed in JS). */
export function uiScale(): number {
  return current;
}

/** Sets --ui-scale on <html>. Returns the applied scale. */
export function applyScale(pref: UiScalePref, root: HTMLElement = document.documentElement): number {
  current = calcScale(pref, window.devicePixelRatio);
  root.style.setProperty('--ui-scale', String(current));
  return current;
}
