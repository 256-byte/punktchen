// What a pole marker shows (task 3.5, ARCHITECTURE §4.5): one row per
// direction — line badge, headsign, countdown to the next departure.

import type { components } from '../api/schema.gen';
import { nextDeparture } from './departures';
import { type DepartureCell, decodeCell, departureCell, encodeCell } from './realtime';

export type Direction = components['schemas']['Direction'];
export type Mode = components['schemas']['Mode'];

/**
 * Badge colour of a line without its own (API `color` null: most trams and
 * buses, all ferries; DATA_MODEL §7 #7). Tram, bus and ferry are the generic
 * rows of VBB's line colour list (§7 #25); the rest are the usual Berlin
 * colours. Design tokens may refine them (task 7.1).
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

const HEX = /^#([0-9a-f]{6})$/i;

/**
 * Badge text colour: white like on Berlin's signs, unless white has less
 * than 3:1 contrast on the background (WCAG AA for bold text); then black.
 */
export function textColorOn(background: string): '#000000' | '#FFFFFF' {
  const m = HEX.exec(background);
  if (!m) {
    return '#FFFFFF';
  }
  const n = parseInt(m[1]!, 16);
  const channel = (shift: number) => {
    const c = ((n >> shift) & 0xff) / 255;
    return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  };
  const luminance = 0.2126 * channel(16) + 0.7152 * channel(8) + 0.0722 * channel(0);
  return 1.05 / (luminance + 0.05) >= 3 ? '#FFFFFF' : '#000000';
}

/** Unique within a pole (the API merges directions by this label, DATA_MODEL §3.2). */
export function directionKey(d: Direction): string {
  return `${d.mode}|${d.line}|${d.headsign}|${d.isRing ? 'ring' : ''}`;
}

/** Rows on a marker; the rest are counted ("+2") and listed in the sheet (3.6). */
export const MARKER_ROWS = 4;

export interface MarkerRow {
  direction: Direction;
  /** Its next departure: countdown, live or timetable, delay (task 4.5). */
  next: DepartureCell;
}

/**
 * The rows of a pole marker: only directions with an upcoming departure, in
 * API order, up to max; `more` counts the other running ones. Directions
 * without one (night lines by day, rail replacement outside its hours,
 * suspended lines) are left to the station sheet (user decision
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
    const cell = cells[i];
    if (cell != null) running.push({ direction, next: decodeCell(cell) });
  });
  return { rows: running.slice(0, max), more: Math.max(0, running.length - max) };
}

/**
 * Each direction's next running departure as an encoded cell (countdown
 * "now" / "3 min" / "14:52", live or timetable, delay; see encodeCell);
 * null when none is left (e.g. a line suspended for weeks, or data too
 * old). Plain strings, so a clock selector over them re-renders only when
 * one changes.
 */
export function nextCells(directions: readonly Direction[], now: number): (string | null)[] {
  return directions.map((d) => {
    const next = nextDeparture(d.departures, now);
    return next ? encodeCell(departureCell(next)) : null;
  });
}
