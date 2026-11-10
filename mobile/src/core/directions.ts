// What a pole marker shows (task 3.5, ARCHITECTURE §4.5): one row per
// direction — line badge, headsign, countdown to the next departure.

import type { components } from '../api/schema.gen';
import { relativeLuminance } from './color';
import { nextDeparture } from './departures';
import { type DepartureCell, decodeCell, departureCell, encodeCell } from './realtime';

export type Direction = components['schemas']['Direction'];
export type Mode = components['schemas']['Mode'];

/**
 * Badge colour of a line without its own (API `color` null: most trams and
 * buses, all ferries; DATA_MODEL §7 #7). Tram, bus and ferry are the generic
 * rows of VBB's line colour list (§7 #25); the rest are the usual Berlin
 * colours. The same in light and dark mode: they are the lines' identity
 * (task 7.1).
 */
export const MODE_COLORS: Record<Mode, string> = {
  regional: '#E30613',
  sbahn: '#008D4F',
  ubahn: '#115D91',
  tram: '#E2001A',
  bus: '#A5027D',
  ferry: '#009BD5',
  other: '#6B7280',
};

/** Badge background: the line's colour, else its mode's. */
export function badgeColor(d: Pick<Direction, 'color' | 'mode'>): string {
  return d.color ?? MODE_COLORS[d.mode];
}

/**
 * Badge text colour: white like on Berlin's signs, unless white has less
 * than 3:1 contrast on the background (WCAG AA for bold text); then black.
 */
export function textColorOn(background: string): '#000000' | '#FFFFFF' {
  const luminance = relativeLuminance(background);
  if (luminance === null) {
    return '#FFFFFF';
  }
  return 1.05 / (luminance + 0.05) >= 3 ? '#FFFFFF' : '#000000';
}

/** Unique within a pole (the API merges directions by this label, DATA_MODEL §3.2). */
export function directionKey(d: Direction): string {
  return `${d.mode}|${d.line}|${d.headsign}|${d.isRing ? 'ring' : ''}`;
}

/**
 * Clockwise (`⟳`, S41) or counter-clockwise (`⟲`, S42) symbol for a Ringbahn
 * direction (`DATA_MODEL.md` §3.2, task 7.3); `null` for non-ring directions.
 */
export function ringSymbol(d: Pick<Direction, 'isRing' | 'line' | 'headsign'>): '⟳' | '⟲' | null {
  if (!d.isRing) return null;
  return d.line === 'S42' || /\bS\s*42\b/i.test(d.headsign) ? '⟲' : '⟳';
}

/**
 * Display headsign of a direction: prefixes `⟳` (clockwise) or `⟲`
 * (counter-clockwise) for Ringbahn lines (`isRing: true`, task 7.3).
 */
export function directionHeadsign(d: Pick<Direction, 'isRing' | 'line' | 'headsign'>): string {
  const symbol = ringSymbol(d);
  if (!symbol || d.headsign.startsWith('⟳') || d.headsign.startsWith('⟲')) {
    return d.headsign;
  }
  return `${symbol} ${d.headsign}`;
}

/**
 * Collects `(mode|line)` keys that have at least one non-terminal return
 * direction whose headsign is not `stationName`.
 */
export function linesWithReturnDirection(
  poles: readonly { directions: readonly Direction[] }[],
  stationName: string,
): Set<string> {
  const lines = new Set<string>();
  for (const pole of poles) {
    for (const d of pole.directions) {
      if (!d.isTerminal && d.headsign !== 'Fahrt endet hier' && d.headsign !== stationName) {
        lines.add(`${d.mode}|${d.line}`);
      }
    }
  }
  return lines;
}

/**
 * Whether a direction is a terminating arrival (`DATA_MODEL.md` §3.2, task 7.3):
 * either flagged `isTerminal` by the API, carrying VBB's termination headsign
 * `"Fahrt endet hier"`, or heading to `stationName` itself when the same
 * `(mode, line)` also has a return direction leaving the station (multi-pole
 * terminals such as S Wannsee where arriving buses call at a drop-off pole
 * before the layover bay; loop lines like 114 whose only headsign is the
 * station itself are kept).
 */
export function isTerminalDirection(
  d: Pick<Direction, 'mode' | 'line' | 'headsign' | 'isTerminal'>,
  stationName?: string,
  returnLines?: ReadonlySet<string>,
): boolean {
  if (d.isTerminal || d.headsign === 'Fahrt endet hier') {
    return true;
  }
  if (stationName && d.headsign === stationName && returnLines?.has(`${d.mode}|${d.line}`)) {
    return true;
  }
  return false;
}

/**
 * Filters out terminating arrival directions (`DATA_MODEL.md` §3.2, task 7.3)
 * so only return directions are shown at terminal stops. Returns `directions`
 * unchanged when no direction is terminal.
 */
export function nonTerminalDirections(
  directions: readonly Direction[],
  station?: { name: string; poles?: readonly { directions: readonly Direction[] }[] },
): Direction[] {
  const poles = station?.poles && station.poles.length > 0 ? station.poles : [{ directions }];
  const returnLines = station ? linesWithReturnDirection(poles, station.name) : undefined;
  if (!directions.some((d) => isTerminalDirection(d, station?.name, returnLines))) {
    return directions as Direction[];
  }
  return directions.filter((d) => !isTerminalDirection(d, station?.name, returnLines));
}

/** Rows on a marker; the rest are counted ("+2") and listed in the sheet (3.6). */
export const MARKER_ROWS = 4;

export interface MarkerRow {
  direction: Direction;
  /** Its next departure: countdown, live or timetable, delay (task 4.5). */
  next: DepartureCell;
}

/**
 * The rows of a pole marker: only non-terminal directions with an upcoming
 * departure, in API order, up to max; `more` counts the other running ones.
 * Directions without one (night lines by day, rail replacement outside its
 * hours, suspended lines) are left to the station sheet (user decision
 * 2026-11-09). No rows: the pole has nothing coming, no marker.
 *
 * cells: nextCells() of the same directions.
 */
export function markerRows(
  directions: readonly Direction[],
  cells: readonly (string | null)[],
  max = MARKER_ROWS,
): { rows: MarkerRow[]; more: number } {
  const running: MarkerRow[] = [];
  directions.forEach((direction, i) => {
    if (isTerminalDirection(direction)) return;
    const cell = cells[i];
    if (cell != null) running.push({ direction, next: decodeCell(cell) });
  });
  return { rows: running.slice(0, max), more: Math.max(0, running.length - max) };
}

/**
 * Each direction's next running departure as an encoded cell (countdown
 * "now" / "3 min" / "14:52", live or timetable, delay; see encodeCell);
 * null when none is left (e.g. a line suspended for weeks, or data too
 * old) or when the direction is a terminal arrival (task 7.3). Plain strings,
 * so a clock selector over them re-renders only when one changes.
 */
export function nextCells(directions: readonly Direction[], now: number): (string | null)[] {
  return directions.map((d) => {
    if (isTerminalDirection(d)) return null;
    const next = nextDeparture(d.departures, now);
    return next ? encodeCell(departureCell(next)) : null;
  });
}

export interface NearbyEmptyState {
  title: string;
  detail: string;
}

/**
 * Empty state for the map when `/v1/nearby` succeeded with nothing to show on
 * markers (task 7.5): either no stops within 500 m, or stops with no non-terminal
 * departures in the 90-minute window. Returns `null` when at least one pole has
 * a non-terminal direction with departures.
 */
export function nearbyEmptyState(
  stations: readonly {
    name: string;
    poles: readonly { directions: readonly Direction[] }[];
  }[],
): NearbyEmptyState | null {
  if (stations.length === 0) {
    return {
      title: 'No stops nearby',
      detail: 'No transit stops within 500 m. Move the map to look around Berlin and Brandenburg.',
    };
  }
  const hasDepartures = stations.some((station) =>
    station.poles.some((pole) =>
      nonTerminalDirections(pole.directions, station).some((d) => d.departures.length > 0),
    ),
  );
  if (!hasDepartures) {
    return {
      title: 'No departures nearby',
      detail: 'Stops in this area have no departures in the next 90 minutes.',
    };
  }
  return null;
}
