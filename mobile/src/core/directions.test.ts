import { describe, expect, it } from 'vitest';

import type { Departure } from './departures';
import {
  badgeColor,
  type Direction,
  directionKey,
  MARKER_ROWS,
  markerDirections,
  MODE_COLORS,
  nextLabels,
  textColorOn,
} from './directions';

const now = Date.parse('2026-11-05T14:30:00+01:00');

function dep(time: string, cancelled = false): Departure {
  return {
    tripId: `t-${time}`,
    serviceDate: '2026-11-05',
    scheduled: `2026-11-05T${time}:00+01:00`,
    predicted: null,
    delaySeconds: null,
    source: 'scheduled',
    cancelled,
  };
}

function direction(patch: Partial<Direction> = {}): Direction {
  return {
    line: 'U6',
    mode: 'ubahn',
    color: '#8C6DAB',
    headsign: 'U Alt-Mariendorf',
    isTerminal: false,
    isRing: false,
    departures: [],
    ...patch,
  };
}

describe('badgeColor', () => {
  it("uses the line's colour, else its mode's", () => {
    expect(badgeColor({ color: '#8C6DAB', mode: 'ubahn' })).toBe('#8C6DAB');
    expect(badgeColor({ color: null, mode: 'bus' })).toBe(MODE_COLORS.bus);
    expect(badgeColor({ color: null, mode: 'other' })).toBe('#6B7280');
  });
});

describe('textColorOn', () => {
  it.each([
    ['#8C6DAB', '#FFFFFF'], // U6 violet
    ['#A5027D', '#FFFFFF'], // bus
    ['#FFD800', '#000000'], // bright yellow
    ['#FFFFFF', '#000000'],
    ['#000000', '#FFFFFF'],
    ['#7DAD4C', '#000000'], // light green (e.g. S1-ish)
    ['#008D4F', '#FFFFFF'],
  ])('%s → %s', (bg, fg) => {
    expect(textColorOn(bg)).toBe(fg);
  });

  it('falls back to white for unexpected values', () => {
    expect(textColorOn('violet')).toBe('#FFFFFF');
  });
});

describe('directionKey', () => {
  it('tells ring and plain directions apart', () => {
    const plain = direction({ line: 'S41', mode: 'sbahn', headsign: 'Ringbahn S 41' });
    expect(directionKey(plain)).toBe('sbahn|S41|Ringbahn S 41|');
    expect(directionKey({ ...plain, isRing: true })).toBe('sbahn|S41|Ringbahn S 41|ring');
  });
});

describe('markerDirections', () => {
  const six = Array.from({ length: 6 }, (_, i) => direction({ line: `M${i}` }));

  it(`shows up to ${MARKER_ROWS} rows and counts the rest`, () => {
    const { shown, more } = markerDirections(six);
    expect(shown.map((d) => d.line)).toEqual(['M0', 'M1', 'M2', 'M3']);
    expect(more).toBe(2);
  });

  it('shows all when they fit', () => {
    expect(markerDirections(six.slice(0, 2))).toEqual({ shown: six.slice(0, 2), more: 0 });
    expect(markerDirections([], 3)).toEqual({ shown: [], more: 0 });
  });
});

describe('nextLabels', () => {
  it('labels the next running departure of each direction', () => {
    const labels = nextLabels(
      [
        direction({ departures: [dep('14:33'), dep('14:43')] }),
        direction({ departures: [dep('14:31', true), dep('15:45')] }), // cancelled skipped
        direction({ departures: [dep('14:20')] }), // left
        direction({ departures: [] }), // e.g. suspended
      ],
      now,
    );
    expect(labels).toEqual(['3 min', '15:45', null, null]);
  });
});
