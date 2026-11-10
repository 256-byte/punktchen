import { describe, expect, it } from 'vitest';

import type { Departure } from './departures';
import type { Direction } from './directions';
import { type Pole, poleTitle, sheetModel, sheetSignature, type Station } from './sheet';

const now = Date.parse('2026-11-05T14:30:00+01:00');
/** Epoch minute of a time on the test day: the sort key in sheetSignature (task 7.7). */
const minuteOf = (hhmm: string) => Date.parse(`2026-11-05T${hhmm}:00+01:00`) / 60_000;

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

/** A realtime departure: scheduled at time, predicted at predicted. */
function live(time: string, predicted: string, delaySeconds: number): Departure {
  return {
    ...dep(time),
    predicted: `2026-11-05T${predicted}:00+01:00`,
    delaySeconds,
    source: 'realtime',
  };
}

function direction(line: string, headsign: string, departures: Departure[]): Direction {
  return { line, mode: 'bus', color: null, headsign, isTerminal: false, isRing: false, departures };
}

function pole(id: string, platform: string | null, directions: Direction[]): Pole {
  return { id, location: { lat: 52.5, lng: 13.4 }, platform, description: null, directions };
}

const station: Station = {
  id: 'de:11000:900009104',
  kind: 'station',
  name: 'S+U Wedding',
  location: { lat: 52.5427, lng: 13.366 },
  modes: ['sbahn', 'bus'],
  poles: [
    pole('p1', '1', [
      direction('S41', 'Ringbahn S 41', [
        dep('14:32'),
        live('14:37', '14:39', 120),
        dep('14:42'),
        dep('14:47'),
      ]),
      direction('N20', 'S Oranienburger Str.', []),
    ]),
    pole('p2', null, [
      direction('N20', 'S Oranienburger Str.', [dep('14:10')]), // left: idle, seen before
      direction('M27', 'S Jungfernheide', [dep('14:33', true), dep('14:43')]),
      direction('247', 'S Nordbahnhof', [dep('14:35', true)]), // only a cancelled one: idle
    ]),
    pole('p3', '2', [direction('RE2', 'Nauen', [])]), // pole without anything running
  ],
};

describe('sheetSignature', () => {
  it('encodes each direction’s next departures: live, delay, cancelled', () => {
    expect(sheetSignature(station, now)).toEqual([
      `${minuteOf('14:32')}|s\t2 min|l+2\t9 min|s\t12 min`, // the late one by its predicted time
      '',
      '',
      `${minuteOf('14:43')}|c\t3 min|s\t13 min`, // sort minute: the next one that runs
      '',
      '',
    ]);
  });

  it('changes only when a label changes', () => {
    expect(sheetSignature(station, now + 20_000)).toEqual(sheetSignature(station, now + 40_000));
    expect(sheetSignature(station, now + 61_000)).not.toEqual(
      sheetSignature(station, now + 40_000),
    );
  });
});

describe('sheetModel', () => {
  const model = sheetModel(station, sheetSignature(station, now));

  it('lists running directions per pole and leaves out poles with nothing coming', () => {
    expect(model.poles.map((p) => p.pole.id)).toEqual(['p1', 'p2']);
    expect(model.poles[0]?.rows.map((r) => r.direction.line)).toEqual(['S41']);
    expect(model.poles[1]?.rows).toEqual([
      {
        key: 'bus|M27|S Jungfernheide|',
        direction: station.poles[1]!.directions[1],
        departures: [
          { label: '3 min', state: 'cancelled', delay: null },
          { label: '13 min', state: 'scheduled', delay: null },
        ],
      },
    ]);
  });

  it('collects directions with nothing coming at the bottom, once each', () => {
    expect(model.idle.map((r) => `${r.direction.line} ${r.platform ?? '-'}`)).toEqual([
      'N20 1',
      '247 -',
      'RE2 2',
    ]);
  });

  it('orders the directions of each platform by next departure, platforms unchanged (task 7.7)', () => {
    const busy: Station = {
      ...station,
      poles: [
        pole('gleis2', '2', [
          direction('M10', 'Warschauer Str.', [dep('21:44')]),
          direction('M27', 'S Jungfernheide', [dep('14:40')]),
          direction('247', 'S Nordbahnhof', [dep('14:31', true), dep('14:30')]), // "now"
          direction('120', 'U Leopoldplatz', [dep('14:30')]), // same minute: API order
        ]),
        pole('gleis1', '1', [
          direction('142', 'S Ostbahnhof', [dep('14:50')]),
          direction('147', 'S Ostbahnhof', [dep('14:35'), dep('15:10')]),
        ]),
      ],
    };
    const rows = (at: number) =>
      sheetModel(busy, sheetSignature(busy, at)).poles.map(
        (p) => `${p.pole.id}: ${p.rows.map((r) => r.direction.line).join(' ')}`,
      );
    expect(rows(now)).toEqual(['gleis2: 247 120 M27 M10', 'gleis1: 147 142']);
    // 14:37: 247 and 120 have left; 147's next is 15:10, so 142 (14:50) moves up.
    expect(rows(now + 7 * 60_000)).toEqual(['gleis2: M27 M10', 'gleis1: 142 147']);
  });

  it('copes with a missing signature (all idle)', () => {
    const empty = sheetModel(station, []);
    expect(empty.poles).toEqual([]);
    expect(empty.idle).toHaveLength(5);
  });

  it('omits directions from idle when the same direction runs on another platform (S+U Wedding S41/S42)', () => {
    const weddingRings: Station = {
      ...station,
      poles: [
        pole('gleis1', '1', [
          { ...direction('S41', 'Ringbahn S 41', [dep('14:32')]), mode: 'sbahn', isRing: true },
          { ...direction('S42', 'Ringbahn S 42', []), mode: 'sbahn', isRing: true },
        ]),
        pole('gleis2', '2', [
          { ...direction('S42', 'Ringbahn S 42', [dep('14:34')]), mode: 'sbahn', isRing: true },
          { ...direction('S41', 'Ringbahn S 41', []), mode: 'sbahn', isRing: true },
        ]),
      ],
    };
    const m = sheetModel(weddingRings, sheetSignature(weddingRings, now));
    expect(m.poles.map((p) => p.rows.map((r) => r.direction.line))).toEqual([['S41'], ['S42']]);
    expect(m.idle).toEqual([]);
  });

  it('shows only the return direction at terminal stops while keeping loop lines (task 7.3)', () => {
    const wannsee: Station = {
      id: 'de:11000:900053301',
      kind: 'station',
      name: 'S Wannsee Bhf',
      location: { lat: 52.421, lng: 13.179 },
      modes: ['sbahn', 'bus'],
      poles: [
        pole('p1', '1', [
          {
            ...direction('S1', 'S Wannsee Bhf', [dep('14:32')]),
            mode: 'sbahn',
            isTerminal: true,
          },
          { ...direction('S1', 'S Oranienburg Bhf', [dep('14:35')]), mode: 'sbahn' },
        ]),
        pole('p2', null, [
          direction('118', 'S Wannsee Bhf', [dep('14:33')]), // multi-pole arrival before layover
          direction('118', 'Rathaus Zehlendorf', [dep('14:38')]),
          direction('114', 'S Wannsee Bhf', [dep('14:40')]), // loop line: only headsign at Wannsee
        ]),
        pole('p3', null, [
          direction('316', 'S Wannsee Bhf', []),
          direction('316', 'Fahrt endet hier', []),
          direction('316', 'Potsdam, Glienicker Brücke', []),
        ]),
      ],
    };
    const m = sheetModel(wannsee, sheetSignature(wannsee, now));
    expect(
      m.poles.map((p) => p.rows.map((r) => `${r.direction.line} -> ${r.direction.headsign}`)),
    ).toEqual([['S1 -> S Oranienburg Bhf'], ['118 -> Rathaus Zehlendorf', '114 -> S Wannsee Bhf']]);
    expect(m.idle.map((r) => `${r.direction.line} -> ${r.direction.headsign}`)).toEqual([
      '316 -> Potsdam, Glienicker Brücke',
    ]);
  });
});

describe('poleTitle', () => {
  const title = (description: string | null, platform: string | null = null, index = 0) =>
    poleTitle({ description, platform }, index);

  it.each([
    ['Bushalt Müllerstraße ht. Lindower Straße', 'Müllerstraße ht. Lindower Straße'],
    ['Tramsteig Stahlheimer Straße ht. Wichertstraße', 'Stahlheimer Straße ht. Wichertstraße'],
    ['Tramhalt Hallesches Ufer', 'Hallesches Ufer'],
    ['Tram- u. Bushalt Breite Straße ht. Berliner Straße', 'Breite Straße ht. Berliner Straße'],
    ['Tram- und Bussteig Alexanderplatz', 'Alexanderplatz'],
    ['Tram-/Bussteig Am Kupfergraben', 'Am Kupfergraben'],
    ['Bus- u. Tramhalt Rathaus', 'Rathaus'],
    ['Tram.-u. Bushalt Wilhelminenhof. ht. Rathenaustr.', 'Wilhelminenhof. ht. Rathenaustr.'],
    ['Bushalt Dorf', 'Dorf'],
  ])('drops the stop prefix: %s', (description, want) => {
    expect(title(description)).toBe(want);
  });

  it.each([
    'S-Bahnsteig Gleis 1',
    'U Bahnsteig Gleis 2',
    'Bahnsteig Gleis 3',
    'Ersatzhalt Osloer Straße ht. Prinzenallee', // rail replacement: keep the word
    'Tram und Ersatzhalt Oberspree',
    'Bushalt', // nothing after the prefix
    'Bussteig A3', // bus station bay: a bare "A3" says nothing
    'VERWEIST: Bushalt Am Bahnhof',
  ])('keeps %s as written', (description) => {
    expect(title(description)).toBe(description);
  });

  it('falls back to the platform, then to the position', () => {
    expect(title(null, '1')).toBe('Platform 1');
    expect(title('  ', '2')).toBe('Platform 2');
    expect(title(null, null, 4)).toBe('Stop 5');
  });
});
