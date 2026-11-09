import { describe, expect, it } from 'vitest';

import type { Departure } from './departures';
import type { Direction } from './directions';
import { type Pole, poleTitle, sheetModel, sheetSignature, type Station } from './sheet';

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
      's\t2 min|l+2\t9 min|s\t12 min', // the late one by its predicted time
      '',
      '',
      'c\t3 min|s\t13 min',
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

  it('copes with a missing signature (all idle)', () => {
    const empty = sheetModel(station, []);
    expect(empty.poles).toEqual([]);
    expect(empty.idle).toHaveLength(5);
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
