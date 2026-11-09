// How a departure's realtime state is shown (task 4.5): live or timetable,
// its delay ("+2", "−1"), cancelled. Marker rows and the station sheet use
// the same cells; encodeCell turns one into a plain string, so a clock
// selector over cells (useNow) changes only when what is shown changes.

import type { EffectiveDeparture } from './departures';

/**
 * scheduled: no realtime data (timetable). onTime / early / late: realtime,
 * by the whole minutes of its delay. cancelled: the trip does not run.
 */
export type DepartureState = 'scheduled' | 'onTime' | 'early' | 'late' | 'cancelled';

export interface DepartureCell {
  /** Countdown to the effective (predicted ?? scheduled) time: "now", "3 min", "14:52". */
  label: string;
  state: DepartureState;
  /** "+2" (late) or "−1" (early, U+2212); null when on time or without realtime. */
  delay: string | null;
}

/**
 * Whole minutes of a delay, toward zero: under a minute late or early
 * counts as on time, like on the station displays.
 */
export function delayMinutes(seconds: number): number {
  return Math.trunc(seconds / 60) || 0; // || 0: no -0
}

/** "+2", "−1"; null for 0. */
export function delayText(minutes: number): string | null {
  if (minutes > 0) return `+${minutes}`;
  if (minutes < 0) return `−${-minutes}`;
  return null;
}

/** The cell of an upcoming departure. */
export function departureCell(
  d: Pick<EffectiveDeparture, 'label' | 'cancelled' | 'source' | 'delaySeconds'>,
): DepartureCell {
  if (d.cancelled) {
    return { label: d.label, state: 'cancelled', delay: null };
  }
  if (d.source !== 'realtime' || d.delaySeconds == null) {
    return { label: d.label, state: 'scheduled', delay: null };
  }
  const minutes = delayMinutes(d.delaySeconds);
  const state = minutes > 0 ? 'late' : minutes < 0 ? 'early' : 'onTime';
  return { label: d.label, state, delay: delayText(minutes) };
}

/** Screen reader text: "3 min, live, 2 min late", "14:52, timetable", "3 min, cancelled". */
export function cellDescription(c: DepartureCell): string {
  switch (c.state) {
    case 'scheduled':
      return `${c.label}, timetable`;
    case 'cancelled':
      return `${c.label}, cancelled`;
    case 'onTime':
      return `${c.label}, live, on time`;
    case 'late':
    case 'early': {
      const n = Math.abs(Number(c.delay?.slice(1)));
      return `${c.label}, live, ${n} min ${c.state}`;
    }
  }
}

const CODES: Record<DepartureState, string> = {
  scheduled: 's',
  onTime: 'o',
  early: 'e',
  late: 'l',
  cancelled: 'c',
};
const STATES = Object.fromEntries(
  Object.entries(CODES).map(([state, code]) => [code, state as DepartureState]),
);

/** A cell as a plain string: state code, delay, tab, label (labels have no tab or "|"). */
export function encodeCell(c: DepartureCell): string {
  return `${CODES[c.state]}${c.delay ?? ''}\t${c.label}`;
}

/** The cell of encodeCell(); unknown input reads as a timetable cell. */
export function decodeCell(s: string): DepartureCell {
  const tab = s.indexOf('\t');
  if (tab < 1) {
    return { label: s, state: 'scheduled', delay: null };
  }
  return {
    label: s.slice(tab + 1),
    state: STATES[s[0]!] ?? 'scheduled',
    delay: s.slice(1, tab) || null,
  };
}
