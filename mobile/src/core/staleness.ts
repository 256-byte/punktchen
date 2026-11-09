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
