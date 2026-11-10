import { describe, expect, it } from 'vitest';

import type { Departure } from './departures';
import {
  badgeColor,
  type Direction,
  directionHeadsign,
  directionKey,
  isTerminalDirection,
  linesWithReturnDirection,
  MARKER_ROWS,
  markerRows,
  MODE_COLORS,
  nearbyEmptyState,
  nextCells,
  nonTerminalDirections,
  ringSymbol,
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

describe('ringSymbol and directionHeadsign (task 7.3)', () => {
  it('prefixes ⟳ for clockwise S41 and ⟲ for counter-clockwise S42', () => {
    const s41 = direction({ line: 'S41', mode: 'sbahn', headsign: 'Ringbahn S 41', isRing: true });
    const s42 = direction({ line: 'S42', mode: 'sbahn', headsign: 'Ringbahn S 42', isRing: true });
    const partialS41 = direction({
      line: 'S41',
      mode: 'sbahn',
      headsign: 'S Ostkreuz Bhf',
      isRing: true,
    });
    const partialS42 = direction({
      line: 'S42',
      mode: 'sbahn',
      headsign: 'S Südkreuz Bhf',
      isRing: true,
    });
    const plain = direction({ line: 'U6', mode: 'ubahn', headsign: 'U Alt-Mariendorf' });

    expect(ringSymbol(s41)).toBe('⟳');
    expect(ringSymbol(s42)).toBe('⟲');
    expect(ringSymbol(plain)).toBeNull();

    expect(directionHeadsign(s41)).toBe('⟳ Ringbahn S 41');
    expect(directionHeadsign(s42)).toBe('⟲ Ringbahn S 42');
    expect(directionHeadsign(partialS41)).toBe('⟳ S Ostkreuz Bhf');
    expect(directionHeadsign(partialS42)).toBe('⟲ S Südkreuz Bhf');
    expect(directionHeadsign({ ...s41, headsign: '⟳ Ringbahn S 41' })).toBe('⟳ Ringbahn S 41');
    expect(directionHeadsign(plain)).toBe('U Alt-Mariendorf');
  });
});

describe('terminal direction filtering (task 7.3)', () => {
  it('drops isTerminal, "Fahrt endet hier", and station-name arrivals when a return direction exists, while keeping loop lines', () => {
    const wannseeDirs = [
      direction({
        line: 'S1',
        mode: 'sbahn',
        headsign: 'S Wannsee Bhf',
        isTerminal: true,
        departures: [dep('14:31')],
      }),
      direction({
        line: 'S1',
        mode: 'sbahn',
        headsign: 'S Oranienburg Bhf',
        departures: [dep('14:35')],
      }),
      direction({
        line: '118',
        mode: 'bus',
        headsign: 'S Wannsee Bhf',
        departures: [dep('14:32')],
      }),
      direction({
        line: '118',
        mode: 'bus',
        headsign: 'Rathaus Zehlendorf',
        departures: [dep('14:38')],
      }),
      direction({
        line: '114',
        mode: 'bus',
        headsign: 'S Wannsee Bhf', // loop line: no other headsign
        departures: [dep('14:40')],
      }),
      direction({
        line: '316',
        mode: 'bus',
        headsign: 'Fahrt endet hier',
        departures: [dep('14:33')],
      }),
    ];

    const returnLines = linesWithReturnDirection([{ directions: wannseeDirs }], 'S Wannsee Bhf');
    expect(returnLines.has('sbahn|S1')).toBe(true);
    expect(returnLines.has('bus|118')).toBe(true);
    expect(returnLines.has('bus|114')).toBe(false);
    expect(isTerminalDirection(wannseeDirs[2]!, 'S Wannsee Bhf', returnLines)).toBe(true);
    expect(isTerminalDirection(wannseeDirs[4]!, 'S Wannsee Bhf', returnLines)).toBe(false);

    const filtered = nonTerminalDirections(wannseeDirs, { name: 'S Wannsee Bhf' });
    expect(filtered.map((d) => `${d.line} -> ${d.headsign}`)).toEqual([
      'S1 -> S Oranienburg Bhf',
      '118 -> Rathaus Zehlendorf',
      '114 -> S Wannsee Bhf',
    ]);

    // Preserves array identity when nothing is filtered.
    const clean = [direction({ line: 'U6', headsign: 'U Alt-Mariendorf' })];
    expect(nonTerminalDirections(clean, { name: 'S+U Wedding' })).toBe(clean);

    // nextCells and markerRows also skip isTerminal and "Fahrt endet hier".
    const cells = nextCells([wannseeDirs[0]!, wannseeDirs[1]!, wannseeDirs[5]!], now);
    expect(cells).toEqual([null, 's\t5 min', null]);
    expect(
      markerRows([wannseeDirs[0]!, wannseeDirs[1]!], ['s\t1 min', 's\t5 min']).rows.map(
        (r) => r.direction.headsign,
      ),
    ).toEqual(['S Oranienburg Bhf']);
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

describe('nearbyEmptyState (task 7.5)', () => {
  it('explains when no stops are within 500 m', () => {
    expect(nearbyEmptyState([])).toEqual({
      title: 'No stops nearby',
      detail: 'No transit stops within 500 m. Move the map to look around Berlin and Brandenburg.',
    });
  });

  it('explains when stops nearby have no departures or only terminal arrivals', () => {
    const stations = [
      {
        name: 'S+U Wedding',
        poles: [
          {
            directions: [
              direction({ line: 'N20', headsign: 'S Oranienburger Str.', departures: [] }),
              direction({
                line: 'U6',
                headsign: 'S+U Wedding',
                isTerminal: true,
                departures: [dep('14:32')],
              }),
            ],
          },
        ],
      },
    ];
    expect(nearbyEmptyState(stations)).toEqual({
      title: 'No departures nearby',
      detail: 'Stops in this area have no departures in the next 90 minutes.',
    });
  });

  it('returns null when at least one non-terminal direction has departures', () => {
    const stations = [
      {
        name: 'S+U Wedding',
        poles: [
          {
            directions: [
              direction({ line: 'U6', headsign: 'U Alt-Mariendorf', departures: [dep('14:32')] }),
            ],
          },
        ],
      },
    ];
    expect(nearbyEmptyState(stations)).toBeNull();
  });
});
