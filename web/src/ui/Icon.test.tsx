import { render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { Icon } from './Icon';
import legacy from '../../../legacy/finuance.html?raw';
import { ICONS, type IconName } from './icons';

describe('Icon', () => {
  it('ports every legacy icon', () => {
    const block = legacy.slice(
      legacy.indexOf('const ICONS = {'),
      legacy.indexOf('\n};', legacy.indexOf('const ICONS = {')),
    );
    const names = [...block.matchAll(/^\s*(\w+):'<svg/gm)].map((m) => m[1]);
    expect(names.length).toBeGreaterThan(30);
    expect(Object.keys(ICONS).sort()).toEqual([...names].sort());
  });

  it.each(Object.keys(ICONS) as IconName[])('%s renders its shapes', (name) => {
    const { container } = render(<Icon name={name} />);
    const svg = container.querySelector('span.ico > svg');
    expect(svg?.getAttribute('viewBox')).toBe('0 0 24 24');
    expect(svg?.children.length).toBe(ICONS[name].length);
  });
});
