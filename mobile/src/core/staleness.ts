// How old the shown data is (ARCHITECTURE §4.3). Age counts from the last
// successful check with the backend: a 200, or a 304 that confirmed the data.

import { MINUTE, SECOND } from './time';

/** Under this age data is shown as is. */
export const AGING_AFTER_MS = 60 * SECOND;
/** From this age realtime values are dimmed and scheduled times emphasized. */
export const STALE_AFTER_MS = 5 * MINUTE;

/** fresh: < 60 s · aging: show "updated X ago" · stale: > 5 min, dim realtime values. */
export type Freshness = 'fresh' | 'aging' | 'stale';

export function freshness(checkedAt: number, now: number): Freshness {
  const age = now - checkedAt;
  if (age < AGING_AFTER_MS) {
    return 'fresh';
  }
  return age <= STALE_AFTER_MS ? 'aging' : 'stale';
}

/** "updated just now" · "updated 3 min ago" · "updated 2 h ago". */
export function updatedAgoLabel(checkedAt: number, now: number): string {
  const minutes = Math.floor(Math.max(0, now - checkedAt) / MINUTE);
  if (minutes < 1) {
    return 'updated just now';
  }
  if (minutes < 60) {
    return `updated ${minutes} min ago`;
  }
  return `updated ${Math.floor(minutes / 60)} h ago`;
}

/**
 * Why the last request failed. unreachable: no HTTP answer (no network, the
 * server down, a timeout). server: the backend answered with an error.
 */
export type Failure = 'unreachable' | 'server';

/** The line above the map that says how far the shown data can be trusted. */
export interface StatusBanner {
  title: string;
  /** Second line; null if the title says it all. */
  detail: string | null;
  /** true: something is wrong (requests fail, or the data is stale). */
  warning: boolean;
}

export const STALE_HINT = 'live times may be out of date';

/**
 * What to say about data last checked at `checkedAt` (ARCHITECTURE §4.3).
 * Nothing while it is fresh and requests work; "Updated X ago" from 60 s;
 * a warning from 5 min, when live values are greyed out; and as soon as a
 * request fails, that the last known data is shown, whatever its age.
 */
export function statusBanner(
  checkedAt: number,
  now: number,
  failure: Failure | null,
): StatusBanner | null {
  const updated = updatedAgoLabel(checkedAt, now);
  const age = freshness(checkedAt, now);
  if (failure !== null) {
    const what = failure === 'unreachable' ? 'No connection' : 'Server problem';
    return {
      title: `${what} — showing last known data`,
      detail: age === 'stale' ? `${updated}, ${STALE_HINT}` : updated,
      warning: true,
    };
  }
  switch (age) {
    case 'fresh':
      return null;
    case 'aging':
      return { title: capitalized(updated), detail: null, warning: false };
    case 'stale':
      return { title: capitalized(updated), detail: capitalized(STALE_HINT), warning: true };
  }
}

function capitalized(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1);
}
