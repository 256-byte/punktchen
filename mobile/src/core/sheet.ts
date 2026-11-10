// What the station sheet shows (task 3.6): per pole the directions with an
// upcoming departure and their next departures; directions with nothing
// coming are collected at the bottom (user decision 2026-11-09).

import type { components } from '../api/schema.gen';
import { upcomingDepartures } from './departures';
import {
  type Direction,
  directionKey,
  isTerminalDirection,
  linesWithReturnDirection,
  markerModes,
  type Mode,
  nonTerminalDirections,
} from './directions';
import { type DepartureCell, decodeCell, departureCell, encodeCell } from './realtime';
import { MINUTE } from './time';

export type Station = components['schemas']['Station'];
export type Pole = components['schemas']['Pole'];

/** Departures per direction in the sheet (the API sends up to 10). */
export const SHEET_DEPARTURES = 3;

/**
 * One string per direction of the station (poles in order, then their
 * directions): the epoch minute of its next running departure (the sort key
 * of sheetModel, task 7.7), then its next departures as encoded cells
 * (countdown, live or timetable, delay, cancelled; task 4.5), all joined by
 * "|"; "" when no running departure is left or when the direction is a
 * terminal arrival (task 7.3). Plain strings, so a clock selector over it
 * (useNow) changes only when a label or the next departure changes.
 */
export function sheetSignature(station: Station, now: number, max = SHEET_DEPARTURES): string[] {
  const returnLines = linesWithReturnDirection(station.poles, station.name);
  return station.poles.flatMap((pole) =>
    pole.directions.map((d) => {
      if (isTerminalDirection(d, station.name, returnLines)) {
        return '';
      }
      const upcoming = upcomingDepartures(d.departures, now);
      const next = upcoming.find((e) => !e.cancelled);
      if (!next) {
        return '';
      }
      const cells = upcoming.slice(0, max).map((e) => encodeCell(departureCell(e)));
      return [Math.floor(next.effectiveAt / MINUTE), ...cells].join('|');
    }),
  );
}

export interface SheetRow {
  key: string;
  direction: Direction;
  departures: DepartureCell[];
}

export interface SheetPole {
  pole: Pole;
  rows: SheetRow[];
}

export interface IdleRow {
  key: string;
  direction: Direction;
  /** Platform of the first pole it was seen at. */
  platform: string | null;
}

export interface SheetModel {
  /** Poles with at least one running direction, in API order; rows soonest first. */
  poles: SheetPole[];
  /** Directions with nothing coming, once each, in order of appearance. */
  idle: IdleRow[];
}

/**
 * The sheet's content from the station and its sheetSignature().
 * Terminating arrival directions (task 7.3) are omitted from both active
 * sections and `idle`; directions that are running on another platform of
 * the same station (e.g. S41/S42 at S+U Wedding) are not listed as idle.
 */
export function sheetModel(station: Station, signature: readonly string[]): SheetModel {
  const returnLines = linesWithReturnDirection(station.poles, station.name);
  const poles: SheetPole[] = [];
  const idle: IdleRow[] = [];
  const seenIdle = new Set<string>();
  const runningKeys = new Set<string>();
  let i = 0;
  for (const pole of station.poles) {
    const rows: { row: SheetRow; minute: number }[] = [];
    for (const direction of pole.directions) {
      const sig = signature[i++] ?? '';
      if (isTerminalDirection(direction, station.name, returnLines)) {
        continue;
      }
      const key = directionKey(direction);
      if (sig !== '') {
        runningKeys.add(key);
        const [minute, ...cells] = sig.split('|');
        rows.push({
          row: { key, direction, departures: cells.map(decodeCell) },
          minute: Number(minute),
        });
      } else if (!seenIdle.has(key)) {
        seenIdle.add(key);
        idle.push({ key, direction, platform: pole.platform });
      }
    }
    if (rows.length > 0) {
      // Platforms keep their order; within one, the soonest direction comes first
      // (task 7.7, user decision 2026-11-10). Stable: equal minutes keep API order.
      rows.sort((a, b) => a.minute - b.minute);
      poles.push({ pole, rows: rows.map((r) => r.row) });
    }
  }
  return { poles, idle: idle.filter((r) => !runningKeys.has(r.key)) };
}

/**
 * Every mode that serves the station, for the icons in the sheet's title
 * (task 7.10): all its poles, directions with nothing coming soon too (a
 * night bus still stops here), terminal arrivals left out; in the order of
 * markerModes (regional, S-Bahn, U-Bahn, tram, bus, ferry).
 */
export function stationModes(station: Pick<Station, 'name' | 'poles'>): Mode[] {
  return markerModes(station.poles.flatMap((p) => nonTerminalDirections(p.directions, station)));
}

/**
 * Bus/tram stop prefix of a GTFS stop_desc: from "Bus"/"Tram" to the first
 * word ending in "halt" or "steig" ("Bushalt ", "Tramsteig ", "Tram- u.
 * Bushalt ", "Tram-/Bussteig ", …; DATA_MODEL §7 #28).
 */
const STOP_PREFIX = /^(?:Tram|Bus)[^\n]*?(?:halt|steig)\s+/;

/**
 * Section title of a pole in the sheet: VBB's description without the
 * bus/tram stop prefix ("Müllerstraße ht. Lindower Straße"); rail platforms
 * and rail replacement stops as written ("S-Bahnsteig Gleis 1", "Ersatzhalt
 * …"); without a description "Platform N", else "Stop N" (index from 0).
 */
export function poleTitle(pole: Pick<Pole, 'description' | 'platform'>, index: number): string {
  const description = pole.description?.trim();
  if (description) {
    const prefix = STOP_PREFIX.exec(description)?.[0];
    const rest = prefix === undefined ? '' : description.slice(prefix.length);
    // Keep rail replacement stops, and bus station bays ("Bussteig A3"):
    // a bare "A3" says nothing.
    if (prefix === undefined || prefix.includes('Ersatz') || rest.length <= 3) {
      return description;
    }
    return rest;
  }
  return pole.platform ? `Platform ${pole.platform}` : `Stop ${index + 1}`;
}
