// Effective departures (DATA_MODEL §9): the time a vehicle actually leaves
// (prediction if there is one, else the schedule), its countdown label, and
// which departures are still worth showing.

import type { components } from '../api/schema.gen';
import { countdownLabel, MINUTE, minutesLeft, parseTimestamp, wallClock } from './time';

export type Departure = components['schemas']['Departure'];

export type EffectiveDeparture = Departure & {
  /** Epoch ms: predicted ?? scheduled. */
  effectiveAt: number;
  /** Whole minutes until effectiveAt (rounded down). */
  minutesLeft: number;
  /** "now" | "3 min" | "14:52". */
  label: string;
};

/**
 * A departure counts as gone this long after its effective time: GTFS times
 * are whole minutes, so a vehicle due at 14:32 leaves during 14:32 and is
 * shown as "now" until 14:33.
 */
export const DEPARTED_AFTER_MS = MINUTE;

/** Whether a departure at effectiveAt has left by now. */
export function hasDeparted(effectiveAt: number, now: number): boolean {
  return now >= effectiveAt + DEPARTED_AFTER_MS;
}

/** The departure with its effective time and label; null if its timestamp is invalid. */
export function effectiveDeparture(d: Departure, now: number): EffectiveDeparture | null {
  const time = d.predicted ?? d.scheduled;
  const at = parseTimestamp(time);
  const clock = wallClock(time);
  if (at === null || clock === null) {
    return null;
  }
  return {
    ...d,
    effectiveAt: at,
    minutesLeft: minutesLeft(at, now),
    label: countdownLabel(at, clock, now),
  };
}

/**
 * The departures still to come, in the order they leave (effective time; ties
 * keep the API order). Departed and invalid ones are dropped; cancelled ones
 * stay, so they can be shown struck through.
 */
export function upcomingDepartures(
  departures: readonly Departure[],
  now: number,
): EffectiveDeparture[] {
  const out: EffectiveDeparture[] = [];
  for (const d of departures) {
    const e = effectiveDeparture(d, now);
    if (e !== null && !hasDeparted(e.effectiveAt, now)) {
      out.push(e);
    }
  }
  return out.sort((a, b) => a.effectiveAt - b.effectiveAt);
}

/** The next departure that runs: cancelled ones are not counted as "next". */
export function nextDeparture(
  departures: readonly Departure[],
  now: number,
): EffectiveDeparture | undefined {
  return upcomingDepartures(departures, now).find((d) => !d.cancelled);
}

/**
 * Effective time of the earliest next departure over several lists (e.g. all
 * directions of a response); null if none runs. The fetch policy refetches
 * once it has left.
 */
export function earliestDepartureAt(
  lists: Iterable<readonly Departure[]>,
  now: number,
): number | null {
  let earliest: number | null = null;
  for (const list of lists) {
    const next = nextDeparture(list, now);
    if (next !== undefined && (earliest === null || next.effectiveAt < earliest)) {
      earliest = next.effectiveAt;
    }
  }
  return earliest;
}
