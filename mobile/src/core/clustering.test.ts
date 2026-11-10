import { describe, expect, it } from 'vitest';

import {
  clusterStations,
  compactLines,
  mergeDirections,
  sameZoomSpec,
  type Station,
  zoomSpec,
} from './clustering';
import type { Departure } from './departures';
import type { Direction } from './directions';

function dep(tripId: string, time = '14:35'): Departure {
  return {
    tripId,
    serviceDate: '2026-11-05',
    scheduled: `2026-11-05T${time}:00+01:00`,
    predicted: null,
    delaySeconds: null,
    source: 'scheduled',
    cancelled: false,
  };
}

function dir(
  line: string,
  headsign: string,
  mode: Direction['mode'] = 'bus',
  departures: Departure[] = [dep(`${line}-${headsign}`)],
): Direction {
  return { line, mode, color: null, headsign, isTerminal: false, isRing: false, departures };
}

// Real S+U Wedding coordinates from DATA_MODEL §7 #23 / nearby-wedding-500m.golden.json.
const weddingStation: Station = {
  id: 'de:11000:900009104',
  kind: 'station',
  name: 'S+U Wedding',
  location: { lat: 52.542732, lng: 13.36606 },
  modes: ['sbahn', 'ubahn', 'bus'],
  poles: [
    {
      id: 'de:11000:900009104::3',
      location: { lat: 52.542915, lng: 13.365584 },
      platform: null,
      description: 'Bushalt Müllerstraße',
      directions: [dir('120', 'Hbf'), dir('N20', 'Night', 'bus', [])],
    },
    {
      id: 'de:11000:900009104::4',
      location: { lat: 52.54281, lng: 13.365222 }, // 27 m from ::3
      platform: null,
      description: 'Bushalt Müllerstraße',
      directions: [dir('120', 'Frohnau'), dir('147', 'Ostbahnhof')],
    },
    {
      id: 'de:11000:900009104::2',
      location: { lat: 52.543092, lng: 13.36509 },
      platform: '2',
      description: null,
      directions: [dir('U6', 'Kurt-Schumacher-Platz', 'ubahn')],
    },
    {
      id: 'de:11000:900009104::1',
      location: { lat: 52.543028, lng: 13.364987 }, // 10 m from ::2
      platform: '1',
      description: null,
      directions: [dir('U6', 'Alt-Mariendorf', 'ubahn')],
    },
    {
      id: 'de:11000:900009104:1:51',
      location: { lat: 52.543102, lng: 13.367604 },
      platform: '1',
      description: null,
      directions: [dir('S41', 'Ringbahn S 41', 'sbahn')],
    },
    {
      id: 'de:11000:900009104:1:50',
      location: { lat: 52.543042, lng: 13.367693 }, // 9 m from :1:51
      platform: '2',
      description: null,
      directions: [dir('S42', 'Ringbahn S 42', 'sbahn')],
    },
    {
      id: 'de:11000:900009104::idle',
      location: { lat: 52.542, lng: 13.365 },
      platform: null,
      description: null,
      directions: [dir('N6', 'Night only', 'bus', [])],
    },
  ],
};

// Alexanderplatz density fixture: main hub with 10 poles + two neighbour stops
// within 100 m (Memhardstr. and Gontardstr.) + Rotes Rathaus 360 m away.
const alexHub: Station = {
  id: 'de:11000:900100003',
  kind: 'station',
  name: 'S+U Alexanderplatz',
  location: { lat: 52.52151, lng: 13.41127 },
  modes: ['regional', 'sbahn', 'ubahn', 'tram', 'bus'],
  poles: [
    {
      id: 'alex::bus1',
      location: { lat: 52.5218, lng: 13.411 },
      platform: null,
      description: null,
      directions: [dir('100', 'S+U Zoologischer Garten'), dir('200', 'Hertzallee')],
    },
    {
      id: 'alex::bus2',
      location: { lat: 52.5219, lng: 13.4112 },
      platform: null,
      description: null,
      directions: [dir('100', 'S+U Alexanderplatz'), dir('300', 'Warschauer Str.')],
    },
    {
      id: 'alex::tram1',
      location: { lat: 52.5214, lng: 13.4118 },
      platform: null,
      description: null,
      directions: [dir('M4', 'Falkenberg', 'tram'), dir('M5', 'Hohenschönhausen', 'tram')],
    },
    {
      id: 'alex::u2',
      location: { lat: 52.5213, lng: 13.4114 },
      platform: '1',
      description: null,
      directions: [dir('U2', 'Pankow', 'ubahn'), dir('U2', 'Ruhleben', 'ubahn')],
    },
    {
      id: 'alex::u5',
      location: { lat: 52.5215, lng: 13.4111 },
      platform: '3',
      description: null,
      directions: [dir('U5', 'Hauptbahnhof', 'ubahn'), dir('U5', 'Hönow', 'ubahn')],
    },
    {
      id: 'alex::u8',
      location: { lat: 52.5216, lng: 13.4109 },
      platform: '5',
      description: null,
      directions: [dir('U8', 'Wittenau', 'ubahn'), dir('U8', 'Hermannstr.', 'ubahn')],
    },
    {
      id: 'alex::sbahn',
      location: { lat: 52.5215, lng: 13.4113 },
      platform: '3',
      description: null,
      directions: [dir('S5', 'Westkreuz', 'sbahn'), dir('S7', 'Potsdam Hbf', 'sbahn')],
    },
    {
      id: 'alex::regio',
      location: { lat: 52.52155, lng: 13.41125 },
      platform: '1',
      description: null,
      directions: [dir('RE1', 'Magdeburg Hbf', 'regional')],
    },
  ],
};

const alexMemhard: Station = {
  id: 'de:11000:900100024',
  kind: 'station',
  name: 'Alexanderplatz/Memhardstr.',
  location: { lat: 52.5222, lng: 13.4114 }, // ~78 m from alexHub
  modes: ['bus'],
  poles: [
    {
      id: 'memhard::1',
      location: { lat: 52.5222, lng: 13.4114 },
      platform: null,
      description: null,
      directions: [dir('248', 'Südkreuz')],
    },
  ],
};

const alexGontard: Station = {
  id: 'de:11000:900100025',
  kind: 'station',
  name: 'Alexanderplatz/Gontardstr.',
  location: { lat: 52.52085, lng: 13.4102 }, // ~103 m from alexHub
  modes: ['tram'],
  poles: [
    {
      id: 'gontard::1',
      location: { lat: 52.52085, lng: 13.4102 },
      platform: null,
      description: null,
      directions: [dir('M2', 'Heinersdorf', 'tram')],
    },
  ],
};

const rotesRathaus: Station = {
  id: 'de:11000:900100004',
  kind: 'station',
  name: 'U Rotes Rathaus',
  location: { lat: 52.5187, lng: 13.4084 }, // ~368 m from alexHub
  modes: ['ubahn', 'bus'],
  poles: [
    {
      id: 'rathaus::1',
      location: { lat: 52.5187, lng: 13.4084 },
      platform: '1',
      description: null,
      directions: [dir('U5', 'Hauptbahnhof', 'ubahn')],
    },
  ],
};

describe('zoomSpec and sameZoomSpec', () => {
  it('maps latitudeDelta into poles, stations and compact buckets', () => {
    expect(zoomSpec()).toEqual({ detail: 'poles', radiusM: 0 });
    expect(zoomSpec(0.001)).toEqual({ detail: 'poles', radiusM: 0 });
    expect(zoomSpec(0.002)).toEqual({ detail: 'poles', radiusM: 15 });
    expect(zoomSpec(0.004)).toEqual({ detail: 'poles', radiusM: 28 });
    expect(zoomSpec(0.0108)).toEqual({ detail: 'stations', radiusM: 120 });
    expect(zoomSpec(0.025)).toEqual({ detail: 'compact', radiusM: 250 });
    expect(zoomSpec(0.05)).toEqual({ detail: 'compact', radiusM: 450 });
  });

  it('keeps the same bucket across pans and small zoom changes', () => {
    expect(sameZoomSpec(0.0108, 0.012)).toBe(true);
    expect(sameZoomSpec(0.0108, 0.004)).toBe(false);
    expect(sameZoomSpec(0.0108, 0.025)).toBe(false);
  });
});

describe('mergeDirections', () => {
  it('returns empty for no poles and preserves single-pole array', () => {
    expect(mergeDirections([])).toEqual([]);
    const single = weddingStation.poles[0]!;
    expect(mergeDirections([single])).toBe(single.directions);
  });

  it('orders rail before bus and interleaves distinct lines before second directions', () => {
    const merged = mergeDirections(alexHub.poles);
    // First directions of each line in mode order: RE1, S5, S7, U2, U5, U8, M4, M5, 100, 200, 300.
    expect(merged.slice(0, 6).map((d) => d.line)).toEqual(['RE1', 'S5', 'S7', 'U2', 'U5', 'U8']);
  });

  it('combines departures when two poles share the same directionKey', () => {
    const p1 = {
      id: 'p1',
      location: { lat: 52.5, lng: 13.4 },
      platform: null,
      description: null,
      directions: [dir('M4', 'Falkenberg', 'tram', [dep('t1', '14:32'), dep('t2', '14:37')])],
    };
    const p2 = {
      id: 'p2',
      location: { lat: 52.5, lng: 13.4 },
      platform: null,
      description: null,
      directions: [dir('M4', 'Falkenberg', 'tram', [dep('t2', '14:37'), dep('t3', '14:42')])],
    };
    const merged = mergeDirections([p1, p2]);
    expect(merged).toHaveLength(1);
    expect(merged[0]!.departures.map((d) => d.tripId)).toEqual(['t1', 't2', 't3']);
  });

  it('drops terminal directions when merging poles (task 7.3)', () => {
    const p1 = {
      id: 'p1',
      location: { lat: 52.5, lng: 13.4 },
      platform: '1',
      description: null,
      directions: [
        { ...dir('U6', 'S+U Wedding', 'ubahn'), isTerminal: true },
        dir('U6', 'U Alt-Mariendorf', 'ubahn'),
      ],
    };
    const p2 = {
      id: 'p2',
      location: { lat: 52.5, lng: 13.4 },
      platform: '2',
      description: null,
      directions: [
        dir('U6', 'Fahrt endet hier', 'ubahn'),
        dir('U6', 'U Kurt-Schumacher-Platz', 'ubahn'),
      ],
    };
    expect(mergeDirections([p1]).map((d) => d.headsign)).toEqual(['U Alt-Mariendorf']);
    expect(mergeDirections([p1, p2]).map((d) => d.headsign)).toEqual([
      'U Alt-Mariendorf',
      'U Kurt-Schumacher-Platz',
    ]);
  });
});

describe('clusterStations across zoom levels', () => {
  const uStop = 'de:11000:900009104::1+de:11000:900009104::2'; // U6 Gleis 1 + 2
  const sStop = 'de:11000:900009104:1:51+de:11000:900009104:1:50'; // S-Bahn Gleis 1 + 2

  it.each([undefined, 0.001, 0.002])(
    'at street zoom (%s) shows S+U Wedding’s U- and S-Bahn as combined stops, each bus stop alone (task 7.8)',
    (latitudeDelta) => {
      const features = clusterStations([weddingStation], latitudeDelta);
      expect(features.map((f) => f.pole.id)).toEqual([
        'de:11000:900009104::3', // both sides of Müllerstr., 27 m apart: bus stops are never one stop
        'de:11000:900009104::4',
        uStop,
        sStop,
      ]); // idle pole: no marker
      expect(features.map((f) => f.sections?.length ?? 1)).toEqual([1, 1, 2, 2]);
      expect(features[3]!.sections!.map((p) => p.directions[0]!.line)).toEqual(['S41', 'S42']);
      // The merged list (screen reader, sheet) still has both U6 directions.
      expect(features[2]!.pole.directions.map((d) => d.headsign)).toEqual([
        'Alt-Mariendorf',
        'Kurt-Schumacher-Platz',
      ]);
    },
  );

  it('at the widest street zoom (0.004) still merges poles within 28 m, bus stops too (task 7.2)', () => {
    const features = clusterStations([weddingStation], 0.004);
    expect(features.map((f) => f.pole.id)).toEqual([
      'de:11000:900009104::3+de:11000:900009104::4',
      uStop,
      sStop,
    ]);
    expect(features.every((f) => f.sections?.length === 2)).toBe(true);
  });

  it('with combined stops switched off, draws street zoom as before 7.8', () => {
    const off = (delta: number) =>
      clusterStations([weddingStation], delta, { combineStops: false });
    // Closest zoom: every pole with something coming is its own marker.
    expect(off(0.001).map((f) => f.pole.id)).toEqual([
      'de:11000:900009104::3',
      'de:11000:900009104::4',
      'de:11000:900009104::2',
      'de:11000:900009104::1',
      'de:11000:900009104:1:51',
      'de:11000:900009104:1:50',
    ]);
    // 15 m: only the 9–10 m twin platforms merge, flat, seed first.
    expect(off(0.002).map((f) => f.pole.id)).toEqual([
      'de:11000:900009104::3',
      'de:11000:900009104::4',
      'de:11000:900009104::2+de:11000:900009104::1',
      'de:11000:900009104:1:51+de:11000:900009104:1:50',
    ]);
    // 28 m: bus pair too; no sections anywhere.
    const wide = off(0.004);
    expect(wide.map((f) => f.pole.id)[0]).toBe('de:11000:900009104::3+de:11000:900009104::4');
    expect([...off(0.001), ...off(0.002), ...wide].some((f) => f.sections)).toBe(false);
    expect(wide[1]!.pole.directions.map((d) => d.headsign)).toEqual([
      'Kurt-Schumacher-Platz',
      'Alt-Mariendorf',
    ]);
  });

  it('at default zoom (0.0108, Alexanderplatz density) merges the hub and its immediate stops while keeping Rotes Rathaus separate', () => {
    // Pass Memhardstr. first to verify that S+U Alexanderplatz still wins as the cluster head.
    const features = clusterStations([alexMemhard, alexHub, alexGontard, rotesRathaus], 0.0108);
    expect(features).toHaveLength(2);
    const [hub, rathaus] = features;
    expect(hub!.station.name).toBe('S+U Alexanderplatz');
    expect(hub!.stationCount).toBe(3);
    expect(hub!.poleCount).toBe(10); // 8 hub + 1 Memhard + 1 Gontard
    expect(hub!.detail).toBe('stations');
    // Top directions on the hub marker are rail lines, not bus 248.
    expect(hub!.pole.directions.slice(0, 4).map((d) => d.line)).toEqual(['RE1', 'S5', 'S7', 'U2']);

    expect(rathaus!.station.name).toBe('U Rotes Rathaus');
    expect(rathaus!.stationCount).toBe(1);
    expect(rathaus!.poleCount).toBe(1);
    expect(rathaus!.pole.id).toBe('rathaus::1');
  });

  it('at overview zoom (0.04) switches to compact detail and merges wider area', () => {
    const features = clusterStations([alexHub, alexMemhard, rotesRathaus], 0.04);
    expect(features).toHaveLength(1);
    expect(features[0]!.detail).toBe('compact');
    expect(features[0]!.station.name).toBe('S+U Alexanderplatz');
    expect(features[0]!.stationCount).toBe(3);
  });
});

describe('combined stops (task 7.8)', () => {
  const pole = (
    id: string,
    lat: number,
    lng: number,
    directions: Direction[],
    platform: string | null = null,
  ) => ({ id, location: { lat, lng }, platform, description: null, directions });
  const station = (name: string, poles: Station['poles']): Station => ({
    id: `de:11000:${name}`,
    kind: 'station',
    name,
    location: poles[0]!.location,
    modes: [],
    poles,
  });
  const ids = (s: Station) =>
    clusterStations([s], 0.001).map((f) => f.sections?.map((p) => p.id) ?? [f.pole.id]);

  // Real coordinates (VBB, 2026-11-10).
  const leopoldplatz = station('U Leopoldplatz', [
    pole('u9-2', 52.546349, 13.35923, [dir('U9', 'U Osloer Str.', 'ubahn')], '2 (U9)'),
    pole('u6-2', 52.546419, 13.359163, [dir('U6', 'U Kurt-Schumacher-Platz', 'ubahn')], '2 (U6)'),
    pole('u9-1', 52.546448, 13.359059, [dir('U9', 'S+U Rathaus Steglitz', 'ubahn')], '1 (U9)'),
    pole('u6-1', 52.546389, 13.359121, [dir('U6', 'U Alt-Mariendorf', 'ubahn')], '1 (U6)'),
    // Bus stop 39 m from the U platforms: another mode, its own marker.
    pole('bus-8', 52.546044, 13.359507, [dir('120', 'S+U Berlin Hauptbahnhof')]),
    // Nazarethkirchstr. (247) and Müllerstr. ht. Schulstr. (120), 28 m apart: they
    // share line 120 only through an idle variant, so they stay apart.
    pole('bus-6', 52.547174, 13.358551, [
      dir('247', 'S Nordbahnhof'),
      dir('120', 'U Leopoldplatz', 'bus', []),
    ]),
    pole('bus-9', 52.546924, 13.358479, [
      dir('120', 'S+U Wittenau'),
      dir('247', 'S Nordbahnhof', 'bus', []),
    ]),
  ]);

  it('joins all U-Bahn platforms of a station, a line’s two directions next to each other', () => {
    expect(ids(leopoldplatz)).toEqual([
      ['u6-1', 'u6-2', 'u9-1', 'u9-2'],
      ['bus-8'],
      ['bus-6'],
      ['bus-9'],
    ]);
  });

  // Real coordinates of S Wedding/Martha-Ndumbe-Platz (bus M27 and 247).
  const corner = (mode: Direction['mode']) =>
    station('S Wedding/Martha-Ndumbe-Platz', [
      pole('m27-a', 52.544164, 13.369588, [dir('M27', 'S+U Jungfernheide', mode)]),
      pole('m27-b', 52.54438, 13.370193, [dir('M27', 'S+U Pankow', mode)]), // 48 m from m27-a
      pole('247-a', 52.544476, 13.370697, [dir('247', 'S Nordbahnhof', mode)]), // 36 m from m27-b
      pole('247-b', 52.544421, 13.371413, [dir('247', 'U Leopoldplatz', mode)]), // 49 m from 247-a
    ]);

  it('never joins bus stops, not even both sides of a street (user decision 2026-11-10)', () => {
    expect(ids(corner('bus'))).toEqual([['m27-a'], ['m27-b'], ['247-a'], ['247-b']]);
  });

  it('joins tram stops on both sides of a street only when they share a running line', () => {
    expect(ids(corner('tram'))).toEqual([
      ['m27-a', 'm27-b'],
      ['247-a', '247-b'],
    ]);
  });

  it('keeps sides of a street apart beyond 50 m, platforms beyond 60 m', () => {
    // 0.0006° latitude ≈ 67 m.
    const far = station('Far', [
      pole('a', 52.5, 13.4, [dir('M4', 'A', 'tram')]),
      pole('b', 52.5006, 13.4, [dir('M4', 'B', 'tram')]),
      pole('u1', 52.5, 13.41, [dir('U8', 'A', 'ubahn')], '1'),
      pole('u2', 52.5006, 13.41, [dir('U8', 'B', 'ubahn')], '2'),
    ]);
    expect(ids(far)).toEqual([['a'], ['b'], ['u1'], ['u2']]);
  });

  it('never joins two stations, even close ones', () => {
    const a = station('A', [pole('a', 52.5, 13.4, [dir('U6', 'X', 'ubahn')], '1')]);
    const b = station('B', [pole('b', 52.50005, 13.4, [dir('U6', 'Y', 'ubahn')], '2')]);
    expect(clusterStations([a, b], 0.001).map((f) => f.pole.id)).toEqual(['a', 'b']);
  });
});

describe('compactLines', () => {
  it('picks up to 4 unique running lines and counts the rest', () => {
    const dirs = mergeDirections(alexHub.poles);
    const cells = dirs.map((_, i) => (i === 0 ? null : 's\t3 min')); // RE1 has no departure left
    const { lines, more } = compactLines(dirs, cells);
    expect(lines.map((d) => d.line)).toEqual(['S5', 'S7', 'U2', 'U5']);
    expect(more).toBe(6); // U8, M4, M5, 100, 200, 300
  });
});
