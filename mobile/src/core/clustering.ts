// Marker clustering and zoom-based detail levels (task 7.2, ARCHITECTURE §4.5).
// Pure TypeScript: takes the stations of /v1/nearby and the map's visible
// latitude span (`Region.latitudeDelta`) and returns the markers to draw.

import type { components } from '../api/schema.gen';
import { type Direction, directionKey, type Mode } from './directions';
import { distanceMeters, type LatLng } from './geo';

export type Station = components['schemas']['Station'];
export type Pole = components['schemas']['Pole'];

/**
 * How much detail a marker shows at the current zoom:
 * - `poles`: street level — individual poles (or very close pole pairs like
 *   opposite platforms 10 m apart), up to 4 departure rows.
 * - `stations`: neighbourhood level (default open zoom, 500 m radius) — one
 *   marker per station (and immediate neighbour stops within 120 m), station
 *   name + up to 4 departure rows across its lines.
 * - `compact`: city overview — station/cluster pill with line badges only,
 *   no headsigns or countdowns.
 */
export type DetailLevel = 'poles' | 'stations' | 'compact';

export interface ZoomSpec {
  detail: DetailLevel;
  /** Clustering radius in metres within this detail level. */
  radiusM: number;
}

/** Max line badges shown on a compact (low-zoom) marker before "+N". */
export const COMPACT_BADGES = 4;

/**
 * Maps the visible vertical span (`Region.latitudeDelta`, 1° ≈ 111.2 km) to a
 * discrete zoom bucket. Discrete buckets mean panning the map without zooming
 * never recomputes clusters or re-renders markers.
 *
 * When `latitudeDelta` is omitted, returns unclustered pole level.
 */
export function zoomSpec(latitudeDelta?: number): ZoomSpec {
  if (latitudeDelta === undefined || latitudeDelta <= 0.0012) {
    return { detail: 'poles', radiusM: 0 };
  }
  if (latitudeDelta <= 0.0025) {
    // ~280 m tall view: merge only twin platforms (e.g. 10 m apart), split bus poles.
    return { detail: 'poles', radiusM: 15 };
  }
  if (latitudeDelta <= 0.006) {
    // ~670 m tall view: group each mode's platforms/poles within a station (~28 m).
    return { detail: 'poles', radiusM: 28 };
  }
  if (latitudeDelta <= 0.018) {
    // Default 500 m radius view (latitudeDelta ≈ 0.0108, ~1.2 km tall): one
    // marker per station, merging immediate neighbour stops within 120 m.
    return { detail: 'stations', radiusM: 120 };
  }
  if (latitudeDelta <= 0.035) {
    return { detail: 'compact', radiusM: 250 };
  }
  return { detail: 'compact', radiusM: 450 };
}

/** True when two `latitudeDelta` values fall into the same zoom bucket. */
export function sameZoomSpec(a?: number, b?: number): boolean {
  const sa = zoomSpec(a);
  const sb = zoomSpec(b);
  return sa.detail === sb.detail && sa.radiusM === sb.radiusM;
}

export interface ClusteredFeature {
  /** Primary station (opened in the bottom sheet when tapped). */
  station: Station;
  /** Synthetic or single pole holding the marker's location and merged directions. */
  pole: Pole;
  /** Number of poles merged into this marker. */
  poleCount: number;
  /** Number of stations merged into this marker. */
  stationCount: number;
  /** Zoom detail level for rendering. */
  detail: DetailLevel;
}

const MODE_RANK: Record<Mode, number> = {
  regional: 0,
  sbahn: 1,
  ubahn: 2,
  tram: 3,
  bus: 4,
  ferry: 5,
  other: 6,
};

/** Whether a pole has any direction with departures in the loaded window. */
function hasDepartures(pole: Pole): boolean {
  return pole.directions.some((d) => d.departures.length > 0);
}

/** Arithmetic mean of coordinates. */
function centroid(points: readonly LatLng[]): LatLng {
  let lat = 0;
  let lng = 0;
  for (const p of points) {
    lat += p.lat;
    lng += p.lng;
  }
  return { lat: lat / points.length, lng: lng / points.length };
}

/**
 * Merges directions from multiple poles: deduplicates by `directionKey`,
 * combines their departures, orders by mode priority (`DATA_MODEL.md` §5.2),
 * and interleaves distinct lines first so one line's two directions do not
 * push other lines (e.g. U-Bahn / S-Bahn at a hub) out of the top rows.
 */
export function mergeDirections(poles: readonly Pole[]): Direction[] {
  if (poles.length === 0) return [];
  if (poles.length === 1) return poles[0]!.directions;

  const byKey = new Map<string, Direction>();
  for (const pole of poles) {
    for (const d of pole.directions) {
      const key = directionKey(d);
      const existing = byKey.get(key);
      if (!existing) {
        byKey.set(key, d);
      } else if (d.departures.length > 0) {
        const seen = new Set(existing.departures.map((x) => `${x.tripId}|${x.serviceDate}`));
        const extra = d.departures.filter((x) => !seen.has(`${x.tripId}|${x.serviceDate}`));
        if (extra.length > 0) {
          byKey.set(key, { ...existing, departures: [...existing.departures, ...extra] });
        }
      }
    }
  }

  const merged = [...byKey.values()].sort((a, b) => MODE_RANK[a.mode] - MODE_RANK[b.mode]);

  // Interleave: first direction of each (mode, line), then remaining directions.
  const firstPerLine: Direction[] = [];
  const rest: Direction[] = [];
  const seenLines = new Set<string>();
  for (const d of merged) {
    const lineId = `${d.mode}|${d.line}`;
    if (!seenLines.has(lineId)) {
      seenLines.add(lineId);
      firstPerLine.push(d);
    } else {
      rest.push(d);
    }
  }
  return [...firstPerLine, ...rest];
}

/**
 * Importance score of a station so that when neighbouring stops merge at
 * Alexanderplatz or Wedding, the main hub ("S+U Alexanderplatz") is always
 * the cluster head rather than an adjacent bus stop.
 */
function stationRank(station: Station, activePoles: readonly Pole[]): number {
  const bestMode = station.modes.reduce((best, m) => Math.min(best, MODE_RANK[m]), MODE_RANK.other);
  return (10 - bestMode) * 1000 + activePoles.length;
}

/**
 * Builds the map markers for `stations` at the zoom given by `latitudeDelta`.
 */
export function clusterStations(
  stations: readonly Station[],
  latitudeDelta?: number,
): ClusteredFeature[] {
  const spec = zoomSpec(latitudeDelta);

  if (spec.detail === 'poles' && spec.radiusM === 0) {
    return stations.flatMap((station) =>
      station.poles.map((pole) => ({
        station,
        pole,
        poleCount: 1,
        stationCount: 1,
        detail: 'poles' as const,
      })),
    );
  }

  if (spec.detail === 'poles') {
    return stations.flatMap((station) => clusterPolesInStation(station, spec.radiusM));
  }

  return clusterByStation(stations, spec.radiusM, spec.detail);
}

/** Street-level clustering: merges nearby active poles within the same station. */
function clusterPolesInStation(station: Station, radiusM: number): ClusteredFeature[] {
  const active = station.poles.filter(hasDepartures);
  if (active.length === 0) return [];

  const used = new Array<boolean>(active.length).fill(false);
  const out: ClusteredFeature[] = [];

  for (let i = 0; i < active.length; i++) {
    if (used[i]) continue;
    used[i] = true;
    const seed = active[i]!;
    const group: Pole[] = [seed];

    for (let j = i + 1; j < active.length; j++) {
      if (used[j]) continue;
      const candidate = active[j]!;
      if (distanceMeters(seed.location, candidate.location) <= radiusM) {
        used[j] = true;
        group.push(candidate);
      }
    }

    if (group.length === 1) {
      out.push({
        station,
        pole: seed,
        poleCount: 1,
        stationCount: 1,
        detail: 'poles',
      });
    } else {
      out.push({
        station,
        pole: {
          id: group.map((p) => p.id).join('+'),
          location: centroid(group.map((p) => p.location)),
          platform: seed.platform,
          description: seed.description,
          directions: mergeDirections(group),
        },
        poleCount: group.length,
        stationCount: 1,
        detail: 'poles',
      });
    }
  }
  return out;
}

/**
 * Station / overview clustering: groups each station's active poles, then
 * merges neighbouring stations within `radiusM` around the highest-ranked hub.
 */
function clusterByStation(
  stations: readonly Station[],
  radiusM: number,
  detail: 'stations' | 'compact',
): ClusteredFeature[] {
  const entries = stations
    .map((station, index) => {
      const activePoles = station.poles.filter(hasDepartures);
      return {
        station,
        index,
        activePoles,
        rank: stationRank(station, activePoles),
      };
    })
    .filter((e) => e.activePoles.length > 0)
    .sort((a, b) => b.rank - a.rank || a.index - b.index);

  const used = new Array<boolean>(entries.length).fill(false);
  const clusters: { order: number; feature: ClusteredFeature }[] = [];

  for (let i = 0; i < entries.length; i++) {
    if (used[i]) continue;
    used[i] = true;
    const head = entries[i]!;
    const allPoles: Pole[] = [...head.activePoles];
    let stationCount = 1;

    for (let j = i + 1; j < entries.length; j++) {
      if (used[j]) continue;
      const other = entries[j]!;
      if (distanceMeters(head.station.location, other.station.location) <= radiusM) {
        used[j] = true;
        allPoles.push(...other.activePoles);
        stationCount++;
      }
    }

    const singlePole = allPoles.length === 1 && stationCount === 1 ? allPoles[0]! : null;
    const pole: Pole = singlePole ?? {
      id: `cluster:${head.station.id}`,
      location: head.station.location,
      platform: null,
      description: null,
      directions: mergeDirections(allPoles),
    };

    clusters.push({
      order: head.index,
      feature: {
        station: head.station,
        pole,
        poleCount: allPoles.length,
        stationCount,
        detail,
      },
    });
  }

  return clusters.sort((a, b) => a.order - b.order).map((c) => c.feature);
}

/**
 * Unique running lines for a `compact` marker: up to `max` directions (one
 * per `(mode, line)` that still has an upcoming departure), plus `more`.
 */
export function compactLines(
  directions: readonly Direction[],
  cells: readonly (string | null)[],
  max = COMPACT_BADGES,
): { lines: Direction[]; more: number } {
  const unique: Direction[] = [];
  const seen = new Set<string>();
  directions.forEach((d, i) => {
    if (cells[i] == null) return;
    const id = `${d.mode}|${d.line}`;
    if (!seen.has(id)) {
      seen.add(id);
      unique.push(d);
    }
  });
  return {
    lines: unique.slice(0, max),
    more: Math.max(0, unique.length - max),
  };
}
