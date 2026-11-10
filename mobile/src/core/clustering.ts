// Marker clustering and zoom-based detail levels (task 7.2, ARCHITECTURE §4.5).
// Pure TypeScript: takes the stations of /v1/nearby and the map's visible
// latitude span (`Region.latitudeDelta`) and returns the markers to draw.

import type { components } from '../api/schema.gen';
import {
  type Direction,
  directionKey,
  isTerminalDirection,
  type Mode,
  nonTerminalDirections,
} from './directions';
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
  /**
   * Combined stop (task 7.8, `poles` zoom only): the poles shown as separate
   * sections of one marker, divided by a line (e.g. both platforms of S+U
   * Wedding's S-Bahn, or both sides of a street). Absent for a single pole.
   */
  sections?: Pole[];
}

/**
 * Combined stop rule (task 7.8, user-approved 2026-11-10). Two active poles
 * of the same station are one stop when their main mode is the same and:
 * - rail (regional, S-Bahn, U-Bahn): they are at most `STOP_RAIL_M` apart —
 *   all platforms of a station's line(s) of that mode, e.g. U6 + U9 at
 *   U Leopoldplatz (4–16 m apart, DATA_MODEL §7);
 * - tram, ferry, other: at most `STOP_STREET_M` apart and they share a line
 *   that has departures — the two sides of a street (Seestr./Amrumer Str.
 *   M13/50, 6 m), not two lines that only meet at a corner (Gerichtstr.
 *   247 / M27, 36 m; U Leopoldplatz Nazarethkirchstr. 247 / Müllerstr. 120,
 *   28 m, which share only idle variants).
 * Bus stops are never joined (user decision 2026-11-10, after seeing it on
 * the iPhone): the two sides of a street are often 30–50 m apart, and one
 * marker between them hides where each stop is.
 * Different modes are never one stop: S and U Wedding stay apart (~180 m)
 * as do U platforms and bus stops next to them.
 */
export const STOP_RAIL_M = 60;
export const STOP_STREET_M = 50;

const RAIL_MODES: ReadonlySet<Mode> = new Set<Mode>(['regional', 'sbahn', 'ubahn']);

const MODE_RANK: Record<Mode, number> = {
  regional: 0,
  sbahn: 1,
  ubahn: 2,
  tram: 3,
  bus: 4,
  ferry: 5,
  other: 6,
};

/** Whether a pole has any non-terminal direction with departures in the loaded window. */
function hasDepartures(pole: Pole): boolean {
  return pole.directions.some((d) => !isTerminalDirection(d) && d.departures.length > 0);
}

/** Strips terminating arrival directions from a station's poles (task 7.3). */
function stationWithoutTerminals(station: Station): Station {
  let changed = false;
  const poles = station.poles.map((pole) => {
    const directions = nonTerminalDirections(pole.directions, station);
    if (directions === pole.directions) return pole;
    changed = true;
    return { ...pole, directions };
  });
  return changed ? { ...station, poles } : station;
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
  if (poles.length === 1) return nonTerminalDirections(poles[0]!.directions);

  const byKey = new Map<string, Direction>();
  for (const pole of poles) {
    for (const d of pole.directions) {
      if (isTerminalDirection(d)) continue;
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

export interface ClusterOptions {
  /**
   * Combined stops at street zoom (task 7.8): both platforms / sides of one
   * stop in one marker with sections. Off: every pole its own marker at the
   * closest zoom and flat merged rows at the wider street zooms, as in 7.2.
   * A switch on the map (user request 2026-11-10); default COMBINE_STOPS_DEFAULT.
   */
  combineStops?: boolean;
}

/** Combined stops are on unless switched off on the map (task 7.8). */
export const COMBINE_STOPS_DEFAULT = true;

/**
 * Builds the map markers for `stations` at the zoom given by `latitudeDelta`.
 */
export function clusterStations(
  stations: readonly Station[],
  latitudeDelta?: number,
  options: ClusterOptions = {},
): ClusteredFeature[] {
  const spec = zoomSpec(latitudeDelta);
  const cleaned = stations.map(stationWithoutTerminals);
  const combineStops = options.combineStops ?? COMBINE_STOPS_DEFAULT;

  if (spec.detail === 'poles') {
    return cleaned.flatMap((station) => clusterPolesInStation(station, spec.radiusM, combineStops));
  }

  return clusterByStation(cleaned, spec.radiusM, spec.detail);
}

/** Most important mode of a pole's directions (its "kind" of stop). */
function poleMode(pole: Pole): Mode {
  let best: Mode = 'other';
  for (const d of pole.directions) {
    if (MODE_RANK[d.mode] < MODE_RANK[best]) best = d.mode;
  }
  return best;
}

/** Whether two poles of one station are one stop (task 7.8, see STOP_RAIL_M). */
export function isSameStop(a: Pole, b: Pole): boolean {
  const mode = poleMode(a);
  if (mode !== poleMode(b)) return false;
  const distance = distanceMeters(a.location, b.location);
  if (RAIL_MODES.has(mode)) return distance <= STOP_RAIL_M;
  if (mode === 'bus' || distance > STOP_STREET_M) return false;
  // Only lines that run now: an idle variant (e.g. 120 ending at U Leopoldplatz)
  // would join a corner stop to a stop of another line.
  const lines = new Set(runningLines(a));
  return runningLines(b).some((line) => lines.has(line));
}

function runningLines(pole: Pole): string[] {
  return pole.directions.filter((d) => d.departures.length > 0).map((d) => `${d.mode}|${d.line}`);
}

const natural = (a: string, b: string) => a.localeCompare(b, undefined, { numeric: true });

/** First running line of a pole in natural order ("M4" < "M10"); "" if none. */
function firstLine(pole: Pole): string {
  return (
    pole.directions
      .filter((d) => d.departures.length > 0)
      .map((d) => d.line)
      .sort(natural)[0] ?? ''
  );
}

/**
 * Order of sections in a combined marker, independent of the API's
 * nearest-first order (which changes as the user walks): mode, first line,
 * platform ("1" < "2" < "10"), id. So a line's two directions sit next to
 * each other: U6 Gleis 1, U6 Gleis 2, U9 Gleis 1, U9 Gleis 2 at U Leopoldplatz.
 */
function compareSections(a: Pole, b: Pole): number {
  return (
    MODE_RANK[poleMode(a)] - MODE_RANK[poleMode(b)] ||
    natural(firstLine(a), firstLine(b)) ||
    natural(a.platform ?? '', b.platform ?? '') ||
    a.id.localeCompare(b.id)
  );
}

/**
 * The combined stops of a station (task 7.8): its active poles joined by
 * isSameStop, transitively (single linkage, so U6 and U9 platforms that are
 * each within reach join one stop). Groups in order of their first pole.
 */
export function stopGroups(poles: readonly Pole[]): Pole[][] {
  const parent = poles.map((_, i) => i);
  const find = (i: number): number => {
    while (parent[i] !== i) {
      parent[i] = parent[parent[i]!]!;
      i = parent[i]!;
    }
    return i;
  };
  for (let i = 0; i < poles.length; i++) {
    for (let j = i + 1; j < poles.length; j++) {
      if (isSameStop(poles[i]!, poles[j]!)) parent[find(j)] = find(i);
    }
  }
  const groups = new Map<number, Pole[]>();
  poles.forEach((pole, i) => {
    const root = find(i);
    const group = groups.get(root);
    if (group) group.push(pole);
    else groups.set(root, [pole]);
  });
  return [...groups.values()];
}

/**
 * Street-level clustering: the station's active poles form combined stops
 * (task 7.8); at the wider street zooms (`radiusM` > 0) stops whose centres
 * are within `radiusM` of a seed stop join it too (task 7.2). A marker with
 * several poles shows each pole as a section.
 */
function clusterPolesInStation(
  station: Station,
  radiusM: number,
  combineStops: boolean,
): ClusteredFeature[] {
  const active = station.poles.filter(hasDepartures);
  if (active.length === 0) return [];

  const stops = combineStops ? stopGroups(active) : active.map((pole) => [pole]);
  const groups = stops.map((poles) => ({
    poles,
    center: centroid(poles.map((p) => p.location)),
  }));
  const used = new Array<boolean>(groups.length).fill(false);
  const out: ClusteredFeature[] = [];

  for (let i = 0; i < groups.length; i++) {
    if (used[i]) continue;
    used[i] = true;
    const seed = groups[i]!;
    const poles = [...seed.poles];
    for (let j = i + 1; j < groups.length; j++) {
      if (used[j]) continue;
      if (radiusM > 0 && distanceMeters(seed.center, groups[j]!.center) <= radiusM) {
        used[j] = true;
        poles.push(...groups[j]!.poles);
      }
    }

    if (poles.length === 1) {
      out.push({ station, pole: poles[0]!, poleCount: 1, stationCount: 1, detail: 'poles' });
      continue;
    }
    if (!combineStops) {
      // As in 7.2: one flat list, seed pole first.
      out.push({
        station,
        pole: {
          id: poles.map((p) => p.id).join('+'),
          location: centroid(poles.map((p) => p.location)),
          platform: seed.poles[0]!.platform,
          description: seed.poles[0]!.description,
          directions: mergeDirections(poles),
        },
        poleCount: poles.length,
        stationCount: 1,
        detail: 'poles',
      });
      continue;
    }
    const sections = poles.sort(compareSections);
    out.push({
      station,
      pole: {
        id: sections.map((p) => p.id).join('+'),
        location: centroid(sections.map((p) => p.location)),
        platform: sections[0]!.platform,
        description: sections[0]!.description,
        directions: mergeDirections(sections),
      },
      poleCount: sections.length,
      stationCount: 1,
      detail: 'poles',
      sections,
    });
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
    if (cells[i] == null || isTerminalDirection(d)) return;
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
