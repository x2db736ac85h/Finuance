import { describe, expect, it } from 'vitest';

import type { TimelineGroup } from '../api/hooks/shared';

import { curveOf } from './SpendCurve';

const g = (over: Partial<TimelineGroup>): TimelineGroup => ({
  id: '00000000-0000-4000-8000-000000000001',
  source: 'personal',
  name: 'G',
  color: 1,
  kind: 'personal',
  currency: 'PLN',
  spentMinor: 0,
  count: 0,
  days: 0,
  perDayMinor: 0,
  ...over,
});

describe('the spending curve of all groups', () => {
  it('adds the groups up in the order they began', () => {
    expect(
      curveOf(
        [
          g({ firstOn: '2026-06-02', spentMinor: 9000 }),
          g({ firstOn: '2025-08-01', spentMinor: 40000 }),
          g({ firstOn: '2026-06-02', spentMinor: 1000 }), // the same day: one step
        ],
        'PLN',
      ),
    ).toEqual([
      { on: '2025-08-01', totalMinor: 40000 },
      { on: '2026-06-02', totalMinor: 50000 },
    ]);
  });

  it('leaves out what can’t be added: another currency, no expenses, no date', () => {
    expect(
      curveOf(
        [
          g({ firstOn: '2025-01-01', spentMinor: 500, currency: 'EUR' }),
          g({ firstOn: '2025-02-01', spentMinor: 0 }),
          g({ spentMinor: 700 }),
          g({ firstOn: '2025-03-01', spentMinor: 300 }),
        ],
        'PLN',
      ),
    ).toEqual([{ on: '2025-03-01', totalMinor: 300 }]);
    expect(curveOf([], 'PLN')).toEqual([]);
  });
});
