// Time helpers for the API's absolute timestamps (ADR-0007): parsing,
// countdowns and labels. All times are epoch milliseconds; "now" is always
// passed in, so every function is deterministic.

export const SECOND = 1000;
export const MINUTE = 60 * SECOND;

// RFC 3339 date-time as the API sends it: 2026-11-05T14:32:00+01:00.
const TIMESTAMP = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(\.\d+)?(Z|[+-]\d{2}:\d{2})$/;

/** Epoch ms of an API timestamp; null if it is not a valid RFC 3339 date-time. */
export function parseTimestamp(value: string): number | null {
  if (!TIMESTAMP.test(value)) {
    return null;
  }
  const ms = Date.parse(value);
  return Number.isNaN(ms) ? null : ms;
}

/**
 * "HH:mm" as written in the timestamp, i.e. in the timezone of its offset.
 * The API sends Berlin time, so this is Berlin time whatever the device's
 * timezone is, without depending on Intl timezone support.
 */
export function wallClock(value: string): string | null {
  const m = TIMESTAMP.exec(value);
  return m ? `${m[4]}:${m[5]}` : null;
}

/** Whole minutes from now until at, rounded down (negative once at has passed). */
export function minutesLeft(at: number, now: number): number {
  return Math.floor((at - now) / MINUTE);
}

/** Countdowns up to this many minutes; later departures show their clock time. */
export const MAX_COUNTDOWN_MINUTES = 59;

/**
 * Display label of a departure (DATA_MODEL §9): under 1 min "now", up to
 * 59 min "N min", from 60 min the clock time "HH:mm".
 */
export function countdownLabel(at: number, clock: string, now: number): string {
  const minutes = minutesLeft(at, now);
  if (minutes < 1) {
    return 'now';
  }
  if (minutes <= MAX_COUNTDOWN_MINUTES) {
    return `${minutes} min`;
  }
  return clock;
}

/** Device clocks closer than this to the server's are trusted as they are. */
export const MAX_CLOCK_SKEW_MS = 30 * SECOND;

/**
 * Correction to add to the device clock (ADR-0007): server time minus device
 * time when a response arrived, if they differ by more than 30 s, else 0.
 * serverTime is the response's meta.generatedAt; receivedAt the device time
 * when the response arrived.
 */
export function clockOffset(serverTime: string, receivedAt: number): number {
  const server = parseTimestamp(serverTime);
  if (server === null) {
    return 0;
  }
  const offset = server - receivedAt;
  return Math.abs(offset) > MAX_CLOCK_SKEW_MS ? offset : 0;
}
