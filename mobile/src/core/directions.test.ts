import { describe, expect, it } from 'vitest';

import type { Departure } from './departures';
import {
  badgeColor,
  type Direction,
  directionKey,
  MARKER_ROWS,
  markerRows,
  MODE_COLORS,
  nextCells,
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

describe('markerRows', () => {
  const six = Array.from({ length: 6 }, (_, i) => direction({ line: `M${i}` }));
  const cell = (label: string) => `s\t${label}`;

  it(`shows up to ${MARKER_ROWS} running directions and counts the other running ones`, () => {
    const { rows, more } = markerRows(
      six,
      ['1 min', null, '2 min', '3 min', '4 min', '5 min'].map((l) => l && cell(l)),
    );
    expect(rows.map((r) => `${r.direction.line} ${r.next.label}`)).toEqual([
      'M0 1 min',
      'M2 2 min',
      'M3 3 min',
      'M4 4 min',
    ]);
    expect(more).toBe(1);
  });

  it('leaves out directions with nothing coming', () => {
    const { rows, more } = markerRows(six.slice(0, 3), [null, 'l+2\t14:52', null]);
    expect(rows).toEqual([
      { direction: six[1], next: { label: '14:52', state: 'late', delay: '+2' } },
    ]);
    expect(more).toBe(0);
    expect(markerRows(six.slice(0, 2), [null, null])).toEqual({ rows: [], more: 0 });
    expect(markerRows([], [], 3)).toEqual({ rows: [], more: 0 });
  });
});

describe('nextCells', () => {
  it('encodes the next running departure of each direction', () => {
    const late: Departure = {
      ...dep('14:31'),
      predicted: '2026-11-05T14:34:00+01:00',
      delaySeconds: 180,
      source: 'realtime',
    };
    const cells = nextCells(
      [
        direction({ departures: [dep('14:33'), dep('14:43')] }),
        direction({ departures: [dep('14:31', true), dep('15:45')] }), // cancelled skipped
        direction({ departures: [dep('14:20')] }), // left
        direction({ departures: [] }), // e.g. suspended
        direction({ departures: [late, dep('14:33')] }), // ordered by predicted time
      ],
      now,
    );
    expect(cells).toEqual(['s\t3 min', 's\t15:45', null, null, 's\t3 min']);
    expect(nextCells([direction({ departures: [late] })], now)).toEqual(['l+3\t4 min']);
  });
});

describe('ten minutes without new data (task 6.3)', () => {
  const at = (hhmm: string) => Date.parse(`2026-11-05T${hhmm}:00+01:00`);
  const dep = (time: string, predicted: string | null = null): Direction['departures'][number] => ({
    tripId: `t-${time}`,
    serviceDate: '2026-11-05',
    scheduled: `2026-11-05T${time}:00+01:00`,
    predicted: predicted === null ? null : `2026-11-05T${predicted}:00+01:00`,
    delaySeconds: predicted === null ? null : 120,
    source: predicted === null ? 'scheduled' : 'realtime',
    cancelled: false,
  });
  const direction = (line: string, departures: Direction['departures']): Direction => ({
    line,
    mode: 'bus',
    color: null,
    headsign: line,
    isTerminal: false,
    isRing: false,
    departures,
  });
  // Fetched at 14:30, as /v1/nearby sends it: three departures per direction.
  const fetched = [
    direction('U6', [dep('14:32'), dep('14:37'), dep('14:42')]),
    direction('M27', [dep('14:33'), dep('14:38', '14:40'), dep('14:53')]),
    direction('247', [dep('14:31'), dep('14:36'), dep('14:39')]),
    direction('N20', []),
  ];
  const labels = (now: number) =>
    markerRows(fetched, nextCells(fetched, now)).rows.map(
      (r) => `${r.direction.line} ${r.next.label}${r.next.delay ?? ''}`,
    );

  it('shows the data as fetched', () => {
    expect(labels(at('14:30'))).toEqual(['U6 2 min', 'M27 3 min', '247 1 min']);
  });

  it('drops what has left and counts down to what is still to come', () => {
    // 14:40: U6 14:32 and 14:37 are gone; M27 runs to its predicted 14:40
    // ("now" for a minute); every 247 has left, so its row is gone.
    expect(labels(at('14:40'))).toEqual(['U6 2 min', 'M27 now+2']);
    expect(labels(at('14:41'))).toEqual(['U6 1 min', 'M27 12 min']);
  });

  it('shows nothing once every known departure has left', () => {
    expect(labels(at('14:54'))).toEqual([]);
  });
});
