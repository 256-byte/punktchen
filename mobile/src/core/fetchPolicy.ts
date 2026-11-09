// When to ask the backend for new data (ARCHITECTURE §4.2). A pure decision
// over the current state; fetchLoop.ts runs it for useNearby (task 5.4)
// on every relevant event and again at `until`.

import type { components } from '../api/schema.gen';
import { DEPARTED_AFTER_MS, type Departure, hasDeparted } from './departures';
import { distanceMeters, type LatLng } from './geo';
import { SECOND } from './time';

type NearbyResponse = components['schemas']['NearbyResponse'];

/** Refetch after moving this far from where the data was fetched. */
export const MOVE_THRESHOLD_M = 150;
/** Refetch when the data is this old. */
export const MAX_DATA_AGE_MS = 30 * SECOND;
/** Never send requests closer together than this, whatever triggers them. */
export const MIN_INTERVAL_MS = 10 * SECOND;
/** Wait after the 1st, 2nd, 3rd, 4th+ failed request in a row. */
export const BACKOFF_MS = [5 * SECOND, 10 * SECOND, 20 * SECOND, 60 * SECOND] as const;

export interface FetchState {
  /** App is in the foreground. */
  foreground: boolean;
  /** Where to fetch for (device position or map centre); null if unknown. */
  position: LatLng | null;
  /** The user pulled to refresh. */
  manual: boolean;
  /** Start of the last request, successful or not. */
  lastAttemptAt: number | null;
  /** Failed requests since the last success. */
  consecutiveErrors: number;
  /** The last successful request (200, or 304 confirming the data). */
  last: {
    checkedAt: number;
    position: LatLng;
    /** earliestDepartureAt() of the data right after that request. */
    earliestDepartureAt: number | null;
  } | null;
}

export type FetchReason = 'initial' | 'manual' | 'retry' | 'moved' | 'departed' | 'age';
export type WaitReason = 'background' | 'noPosition' | 'minInterval' | 'backoff' | 'fresh';

export type FetchDecision =
  | { fetch: true; reason: FetchReason }
  /** until: when the decision can change without a new event; null: only on an event. */
  | { fetch: false; reason: WaitReason; until: number | null };

/** Backoff after n failed requests in a row (n ≥ 1). */
export function backoffMs(failures: number): number {
  const i = Math.min(Math.max(failures, 1), BACKOFF_MS.length) - 1;
  return BACKOFF_MS[i]!; // i is within the list
}

/**
 * Whether to fetch now. In order: only in the foreground with a known
 * position; never within 10 s of the last request; after errors wait for
 * the backoff (pull to refresh skips it), then retry; otherwise fetch for
 * the first data, pull to refresh, a move of > 150 m, a passed earliest
 * departure or data at least 30 s old.
 *
 * The 10 s floor applies to retries too, so the first retry after an error
 * comes after 10 s, not 5 s.
 */
export function decideFetch(s: FetchState, now: number): FetchDecision {
  if (!s.foreground) {
    return { fetch: false, reason: 'background', until: null };
  }
  if (s.position === null) {
    return { fetch: false, reason: 'noPosition', until: null };
  }
  if (s.lastAttemptAt !== null && now < s.lastAttemptAt + MIN_INTERVAL_MS) {
    return { fetch: false, reason: 'minInterval', until: s.lastAttemptAt + MIN_INTERVAL_MS };
  }
  if (s.manual) {
    return { fetch: true, reason: 'manual' };
  }
  if (s.consecutiveErrors > 0 && s.lastAttemptAt !== null) {
    const retryAt = s.lastAttemptAt + backoffMs(s.consecutiveErrors);
    return now < retryAt
      ? { fetch: false, reason: 'backoff', until: retryAt }
      : { fetch: true, reason: 'retry' };
  }
  const last = s.last;
  if (last === null) {
    return { fetch: true, reason: 'initial' };
  }
  if (distanceMeters(last.position, s.position) > MOVE_THRESHOLD_M) {
    return { fetch: true, reason: 'moved' };
  }
  if (last.earliestDepartureAt !== null && hasDeparted(last.earliestDepartureAt, now)) {
    return { fetch: true, reason: 'departed' };
  }
  const tooOldAt = last.checkedAt + MAX_DATA_AGE_MS;
  if (now >= tooOldAt) {
    return { fetch: true, reason: 'age' };
  }
  // Check again when the data gets too old or the earliest departure leaves.
  const until =
    last.earliestDepartureAt === null
      ? tooOldAt
      : Math.min(tooOldAt, last.earliestDepartureAt + DEPARTED_AFTER_MS);
  return { fetch: false, reason: 'fresh', until };
}

/**
 * Every direction's departure list in a /v1/nearby answer (all layers that
 * have departures: transit), for earliestDepartureAt().
 */
export function* nearbyDepartureLists(response: NearbyResponse): Generator<readonly Departure[]> {
  for (const station of response.layers.transit?.features ?? []) {
    for (const pole of station.poles) {
      for (const direction of pole.directions) {
        yield direction.departures;
      }
    }
  }
}

/**
 * The point to request data for: stays put until the position moved more
 * than 150 m from it, so small moves (and map pans) reuse one query and its
 * ETag.
 */
export function nextRequestCenter(current: LatLng | null, position: LatLng): LatLng {
  return current !== null && distanceMeters(current, position) <= MOVE_THRESHOLD_M
    ? current
    : position;
}
