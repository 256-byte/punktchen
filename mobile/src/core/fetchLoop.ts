// Runs the fetch policy (task 5.4, ARCHITECTURE §4.2): asks decideFetch
// after every event (position, foreground, pull to refresh, a request that
// ended) and once more at its `until`, with a single timer instead of
// polling. At most one request runs at a time. Clock, timers and the
// request itself are passed in, so this stays free of React Native.

import { decideFetch, type FetchReason, type FetchState } from './fetchPolicy';
import type { LatLng } from './geo';
import { MINUTE } from './time';

/** What a request reports back. */
export type FetchResult =
  | {
      ok: true;
      /** earliestDepartureAt() of the data right after the request. */
      earliestDepartureAt: number | null;
    }
  | { ok: false; error?: string };

export interface FetchLoopOptions {
  /** Device clock (epoch ms); earliestDepartureAt results must be in it too. */
  now: () => number;
  /** Sends the request for the current position (200 or 304 → ok). */
  fetch: (reason: FetchReason) => Promise<FetchResult>;
  setTimer: (run: () => void, ms: number) => unknown;
  clearTimer: (timer: unknown) => void;
  /** Development log: one line per request and per failure. */
  log?: (line: string) => void;
}

export interface FetchLoop {
  /** New inputs; unchanged ones may be left out. */
  update(input: { foreground?: boolean; position?: LatLng | null }): void;
  /** Pull to refresh / "Try again": skips the backoff, not the 10 s floor. */
  refresh(): void;
  /** Stops the timer; a running request is ignored when it ends. */
  dispose(): void;
  /** The policy's state, for tests and logs. */
  state(): Readonly<FetchState>;
  /** Requests started in the last minute (the dev log's number). */
  requestsLastMinute(): number;
}

export function createFetchLoop(o: FetchLoopOptions): FetchLoop {
  const s: FetchState = {
    foreground: false,
    position: null,
    manual: false,
    lastAttemptAt: null,
    consecutiveErrors: 0,
    last: null,
  };
  let running = false;
  let disposed = false;
  let timer: unknown = null;
  const starts: number[] = [];

  const lastMinute = (now: number) => {
    while (starts.length > 0 && starts[0]! <= now - MINUTE) starts.shift();
    return starts.length;
  };

  function schedule(): void {
    if (disposed || running) return; // the request's end schedules again
    if (timer !== null) {
      o.clearTimer(timer);
      timer = null;
    }
    const now = o.now();
    const d = decideFetch(s, now);
    if (d.fetch) {
      void run(d.reason, now);
    } else if (d.until !== null) {
      timer = o.setTimer(
        () => {
          timer = null;
          schedule();
        },
        Math.max(0, d.until - now),
      );
    }
  }

  async function run(reason: FetchReason, startedAt: number): Promise<void> {
    running = true;
    s.lastAttemptAt = startedAt;
    s.manual = false;
    const position = s.position!; // decideFetch fetches only with a position
    starts.push(startedAt);
    o.log?.(`[nearby] request (${reason}), ${lastMinute(startedAt)} in the last minute`);

    let result: FetchResult;
    try {
      result = await o.fetch(reason);
    } catch (e) {
      result = { ok: false, error: e instanceof Error ? e.message : String(e) };
    }
    running = false;
    if (disposed) return;
    if (result.ok) {
      s.consecutiveErrors = 0;
      s.last = { checkedAt: o.now(), position, earliestDepartureAt: result.earliestDepartureAt };
    } else {
      s.consecutiveErrors++;
      o.log?.(
        `[nearby] failed (${s.consecutiveErrors} in a row): ${result.error ?? 'unknown error'}`,
      );
    }
    schedule();
  }

  return {
    update({ foreground, position }) {
      let changed = false;
      if (foreground !== undefined && foreground !== s.foreground) {
        s.foreground = foreground;
        changed = true;
      }
      if (position !== undefined && !samePosition(position, s.position)) {
        s.position = position;
        changed = true;
      }
      if (changed) schedule();
    },
    refresh() {
      s.manual = true;
      schedule();
    },
    dispose() {
      disposed = true;
      if (timer !== null) o.clearTimer(timer);
      timer = null;
    },
    state: () => s,
    requestsLastMinute: () => lastMinute(o.now()),
  };
}

function samePosition(a: LatLng | null, b: LatLng | null): boolean {
  return a === b || (a !== null && b !== null && a.lat === b.lat && a.lng === b.lng);
}
