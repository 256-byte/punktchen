// What the station sheet shows (task 3.6): per pole the directions with an
// upcoming departure and their next departures; directions with nothing
// coming are collected at the bottom (user decision 2026-11-09).

import type { components } from '../api/schema.gen';
import { upcomingDepartures } from './departures';
import { type Direction, directionKey } from './directions';
import { type DepartureCell, decodeCell, departureCell, encodeCell } from './realtime';

export type Station = components['schemas']['Station'];
export type Pole = components['schemas']['Pole'];

/** Departures per direction in the sheet (the API sends up to 10). */
export const SHEET_DEPARTURES = 3;

/**
 * One string per direction of the station (poles in order, then their
 * directions): its next departures as encoded cells (countdown, live or
 * timetable, delay, cancelled; task 4.5), joined by "|"; "" when no
 * running departure is left. Plain strings, so a clock
 * selector over it (useNow) changes only when a label changes.
 */
export function sheetSignature(station: Station, now: number, max = SHEET_DEPARTURES): string[] {
  return station.poles.flatMap((pole) =>
    pole.directions.map((d) => {
      const upcoming = upcomingDepartures(d.departures, now);
      if (!upcoming.some((e) => !e.cancelled)) {
        return '';
      }
      return upcoming
        .slice(0, max)
        .map((e) => encodeCell(departureCell(e)))
        .join('|');
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
  /** Poles with at least one running direction, in API order. */
  poles: SheetPole[];
  /** Directions with nothing coming, once each, in order of appearance. */
  idle: IdleRow[];
}

/** The sheet's content from the station and its sheetSignature(). */
export function sheetModel(station: Station, signature: readonly string[]): SheetModel {
  const poles: SheetPole[] = [];
  const idle: IdleRow[] = [];
  const seenIdle = new Set<string>();
  let i = 0;
  for (const pole of station.poles) {
    const rows: SheetRow[] = [];
    for (const direction of pole.directions) {
      const sig = signature[i++] ?? '';
      const key = directionKey(direction);
      if (sig !== '') {
        rows.push({ key, direction, departures: sig.split('|').map(decodeCell) });
      } else if (!seenIdle.has(key)) {
        seenIdle.add(key);
        idle.push({ key, direction, platform: pole.platform });
      }
    }
    if (rows.length > 0) {
      poles.push({ pole, rows });
    }
  }
  return { poles, idle };
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
