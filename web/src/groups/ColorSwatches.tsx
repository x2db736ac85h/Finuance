// The eight group colours as a radio group (legacy .gpal): a personal group's form and, since
// Phase 26 (U9), a shared group's settings.

import { colorVar, GROUP_COLORS } from './colors';

export function ColorSwatches({ value, onChange }: { value: number; onChange: (color: number) => void }) {
  return (
    <div className="gpal" role="radiogroup" aria-label="Colour">
      {GROUP_COLORS.map((_, k) => (
        <button
          key={k}
          type="button"
          role="radio"
          aria-checked={k === value}
          aria-label={`Colour ${String(k + 1)}`}
          title={`Colour ${String(k + 1)}`}
          className={k === value ? 'gsw on' : 'gsw'}
          style={colorVar(k)}
          onClick={() => {
            onChange(k);
          }}
        />
      ))}
    </div>
  );
}
