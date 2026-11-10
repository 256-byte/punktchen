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
});

describe('clusterStations across zoom levels', () => {
  it('without latitudeDelta returns every pole unclustered', () => {
    const features = clusterStations([weddingStation]);
    expect(features).toHaveLength(7);
    expect(features.every((f) => f.detail === 'poles' && f.poleCount === 1)).toBe(true);
  });

  it('at street zoom (0.004) splits S+U Wedding into Bus, U-Bahn and S-Bahn groups and drops idle poles', () => {
    const features = clusterStations([weddingStation], 0.004);
    expect(features).toHaveLength(3);
    expect(features.map((f) => f.pole.id)).toEqual([
      'de:11000:900009104::3+de:11000:900009104::4',
      'de:11000:900009104::2+de:11000:900009104::1',
      'de:11000:900009104:1:51+de:11000:900009104:1:50',
    ]);
    // U-Bahn twin platforms now show both directions on one marker instead of overlapping.
    expect(features[1]!.pole.directions.map((d) => d.headsign)).toEqual([
      'Kurt-Schumacher-Platz',
      'Alt-Mariendorf',
    ]);
  });

  it('at closer street zoom (0.002) separates the two bus poles while keeping 10 m twin platforms together', () => {
    const features = clusterStations([weddingStation], 0.002);
    expect(features).toHaveLength(4);
    expect(features[0]!.pole.id).toBe('de:11000:900009104::3');
    expect(features[1]!.pole.id).toBe('de:11000:900009104::4');
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

describe('compactLines', () => {
  it('picks up to 4 unique running lines and counts the rest', () => {
    const dirs = mergeDirections(alexHub.poles);
    const cells = dirs.map((_, i) => (i === 0 ? null : 's\t3 min')); // RE1 has no departure left
    const { lines, more } = compactLines(dirs, cells);
    expect(lines.map((d) => d.line)).toEqual(['S5', 'S7', 'U2', 'U5']);
    expect(more).toBe(6); // U8, M4, M5, 100, 200, 300
  });
});
