// What a pole marker shows (task 3.5, ARCHITECTURE §4.5): one row per
// direction — line badge, headsign, countdown to the next departure.

import type { components } from '../api/schema.gen';
import { relativeLuminance } from './color';
import { nextDeparture } from './departures';
import { type DepartureCell, decodeCell, departureCell, encodeCell } from './realtime';
import { MINUTE } from './time';

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

/** Modes with an icon (task 7.10), in the order of DATA_MODEL §5.2; `other` has none. */
export const ICON_MODES: readonly Mode[] = ['regional', 'sbahn', 'ubahn', 'tram', 'bus', 'ferry'];

/**
 * The modes a marker stands for, for its header icons (task 7.10): every mode
 * of its directions, terminal arrivals left out, in ICON_MODES order. At
 * street zoom a bus stop gives [bus], at a merged station or cluster e.g.
 * [sbahn, ubahn, bus].
 */
export function markerModes(directions: readonly Direction[]): Mode[] {
  const present = new Set<Mode>();
  for (const d of directions) {
    if (!isTerminalDirection(d)) present.add(d.mode);
  }
  return ICON_MODES.filter((m) => present.has(m));
}

/** Spoken name of a mode, for screen readers. */
export const MODE_NAMES: Record<Mode, string> = {
  regional: 'Regional train',
  sbahn: 'S-Bahn',
  ubahn: 'U-Bahn',
  tram: 'Tram',
  bus: 'Bus',
  ferry: 'Ferry',
  other: 'Other',
};

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
 * departure, up to max; `more` counts the other running ones. Directions
 * without one (night lines by day, rail replacement outside its hours,
 * suspended lines) are left to the station sheet (user decision 2026-11-09).
 * No rows: the pole has nothing coming, no marker.
 *
 * Which directions get a row follows the given order (API order, or the
 * mode-first merge of a cluster, task 7.2); the shown rows are then ordered
 * by their next departure, soonest on top ("now", "3 min", "21:44"; task
 * 7.7). Equal minutes keep the given order. The order moves with the clock,
 * because the cells do.
 *
 * cells: nextCells() of the same directions.
 */
export function markerRows(
  directions: readonly Direction[],
  cells: readonly (string | null)[],
  max = MARKER_ROWS,
): { rows: MarkerRow[]; more: number } {
  const running = runningRows(directions, cells);
  return { rows: soonestFirst(running.slice(0, max)), more: Math.max(0, running.length - max) };
}

/**
 * Rows of a combined stop marker (task 7.8). More than MARKER_ROWS: it stands
 * for two to four poles that each had their own marker before.
 */
export const SECTION_MARKER_ROWS = 6;

/**
 * The rows of a combined stop marker (task 7.8), one list per section (pole),
 * empty sections left out. Up to `max` rows are dealt out in turns — each
 * section's first running direction, then each one's second, … — so every
 * platform or street side gets a row before any gets a second. Inside a
 * section the shown rows are ordered soonest first, like markerRows (7.7).
 *
 * sections: each section's directions; cells: nextCells() of all of them, in
 * the same order, concatenated.
 */
export function sectionRows(
  sections: readonly (readonly Direction[])[],
  cells: readonly (string | null)[],
  max = SECTION_MARKER_ROWS,
): { sections: MarkerRow[][]; more: number } {
  let offset = 0;
  const running = sections
    .map((directions) => {
      const rows = runningRows(directions, cells.slice(offset, offset + directions.length));
      offset += directions.length;
      return rows;
    })
    .filter((rows) => rows.length > 0);
  const total = running.reduce((n, rows) => n + rows.length, 0);
  const taken = running.map(() => 0);
  let shown = 0;
  for (let turn = 0; shown < max && shown < total; turn++) {
    running.forEach((rows, s) => {
      if (shown < max && turn < rows.length) {
        taken[s]!++;
        shown++;
      }
    });
  }
  return {
    sections: running.map((rows, s) => soonestFirst(rows.slice(0, taken[s]))),
    more: total - shown,
  };
}

interface RunningRow {
  row: MarkerRow;
  minute: number;
}

/** Non-terminal directions with an upcoming departure, in the given order. */
function runningRows(
  directions: readonly Direction[],
  cells: readonly (string | null)[],
): RunningRow[] {
  const running: RunningRow[] = [];
  directions.forEach((direction, i) => {
    if (isTerminalDirection(direction)) return;
    const cell = cells[i];
    if (cell == null) return;
    const { minute, encoded } = splitNextCell(cell);
    running.push({ row: { direction, next: decodeCell(encoded) }, minute });
  });
  return running;
}

/** Soonest next departure first; equal minutes keep their order (stable sort). */
function soonestFirst(rows: RunningRow[]): MarkerRow[] {
  return [...rows].sort((a, b) => a.minute - b.minute).map((r) => r.row);
}

/**
 * Each direction's next running departure as a plain string: the epoch minute
 * of its effective time (the sort key of markerRows, task 7.7), "|", and the
 * encoded cell (countdown "now" / "3 min" / "14:52", live or timetable, delay;
 * see encodeCell). null when none is left (e.g. a line suspended for weeks,
 * or data too old) or when the direction is a terminal arrival (task 7.3).
 * Plain strings, so a clock selector over them re-renders only when one
 * changes: the minute changes with the departure, not with the clock.
 */
export function nextCells(directions: readonly Direction[], now: number): (string | null)[] {
  return directions.map((d) => {
    if (isTerminalDirection(d)) return null;
    const next = nextDeparture(d.departures, now);
    if (!next) return null;
    return `${Math.floor(next.effectiveAt / MINUTE)}|${encodeCell(departureCell(next))}`;
  });
}

/** Sort minute and encoded cell of a nextCells() string (encoded cells have no "|"). */
function splitNextCell(cell: string): { minute: number; encoded: string } {
  const bar = cell.indexOf('|');
  const minute = bar > 0 ? Number(cell.slice(0, bar)) : NaN;
  if (Number.isNaN(minute)) {
    return { minute: Number.MAX_SAFE_INTEGER, encoded: cell };
  }
  return { minute, encoded: cell.slice(bar + 1) };
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
