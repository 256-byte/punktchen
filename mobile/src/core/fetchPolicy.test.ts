import { describe, expect, it } from 'vitest';

import {
  backoffMs,
  decideFetch,
  type FetchState,
  nearbyDepartureLists,
  nextRequestCenter,
} from './fetchPolicy';
import { MINUTE, SECOND } from './time';

const t0 = Date.parse('2026-11-05T14:30:00+01:00');
const wedding = { lat: 52.5427, lng: 13.366 };
// 0.001° latitude ≈ 111 m, so these are ~140 m and ~160 m north.
const near = { lat: wedding.lat + 0.00126, lng: wedding.lng };
const far = { lat: wedding.lat + 0.00144, lng: wedding.lng };

/** Foreground, data fetched at t0 at Wedding, earliest departure at 14:35. */
function state(patch: Partial<FetchState> = {}): FetchState {
  return {
    foreground: true,
    position: wedding,
    manual: false,
    lastAttemptAt: t0,
    consecutiveErrors: 0,
    last: { checkedAt: t0, position: wedding, earliestDepartureAt: t0 + 5 * MINUTE },
    ...patch,
  };
}

describe('backoffMs', () => {
  it.each([
    [1, 5 * SECOND],
    [2, 10 * SECOND],
    [3, 20 * SECOND],
    [4, 60 * SECOND],
    [25, 60 * SECOND],
    [0, 5 * SECOND],
  ])('%i failures → %i ms', (n, ms) => {
    expect(backoffMs(n)).toBe(ms);
  });
});

describe('decideFetch', () => {
  it('fetches the first data at once', () => {
    expect(decideFetch(state({ lastAttemptAt: null, last: null }), t0)).toEqual({
      fetch: true,
      reason: 'initial',
    });
  });

  it('does nothing in the background', () => {
    const s = state({ foreground: false, last: null, lastAttemptAt: null, manual: true });
    expect(decideFetch(s, t0 + MINUTE)).toEqual({
      fetch: false,
      reason: 'background',
      until: null,
    });
  });

  it('waits for a position', () => {
    expect(decideFetch(state({ position: null, last: null, lastAttemptAt: null }), t0)).toEqual({
      fetch: false,
      reason: 'noPosition',
      until: null,
    });
  });

  it('keeps fresh data until it is 30 s old', () => {
    expect(decideFetch(state(), t0 + 15 * SECOND)).toEqual({
      fetch: false,
      reason: 'fresh',
      until: t0 + 30 * SECOND,
    });
    expect(decideFetch(state(), t0 + 30 * SECOND)).toEqual({ fetch: true, reason: 'age' });
  });

  it('refetches after a move of more than 150 m', () => {
    expect(decideFetch(state({ position: near }), t0 + 15 * SECOND)).toMatchObject({
      fetch: false,
      reason: 'fresh',
    });
    expect(decideFetch(state({ position: far }), t0 + 15 * SECOND)).toEqual({
      fetch: true,
      reason: 'moved',
    });
  });

  it('refetches once the earliest shown departure has left', () => {
    const s = state({
      last: { checkedAt: t0, position: wedding, earliestDepartureAt: t0 + 5 * SECOND },
    });
    // Leaves during its minute: check again at t0 + 65 s, but age wins first.
    expect(decideFetch(s, t0 + 20 * SECOND)).toEqual({
      fetch: false,
      reason: 'fresh',
      until: t0 + 30 * SECOND,
    });

    const departing = state({
      last: { checkedAt: t0, position: wedding, earliestDepartureAt: t0 - 50 * SECOND },
    });
    expect(decideFetch(departing, t0 + 5 * SECOND)).toMatchObject({ reason: 'minInterval' });
    expect(decideFetch(departing, t0 + 10 * SECOND)).toEqual({ fetch: true, reason: 'departed' });
    expect(
      decideFetch(
        state({
          lastAttemptAt: t0 - MINUTE,
          last: { checkedAt: t0, position: wedding, earliestDepartureAt: t0 - 50 * SECOND },
        }),
        t0 + 5 * SECOND,
      ),
    ).toEqual({
      fetch: false,
      reason: 'fresh',
      until: t0 + 10 * SECOND,
    });
  });

  it('without departures only the age counts', () => {
    const s = state({ last: { checkedAt: t0, position: wedding, earliestDepartureAt: null } });
    expect(decideFetch(s, t0 + 29 * SECOND)).toEqual({
      fetch: false,
      reason: 'fresh',
      until: t0 + 30 * SECOND,
    });
  });

  it('never sends requests less than 10 s apart', () => {
    const s = state({ position: far, manual: true });
    expect(decideFetch(s, t0 + 9 * SECOND)).toEqual({
      fetch: false,
      reason: 'minInterval',
      until: t0 + 10 * SECOND,
    });
    expect(decideFetch(s, t0 + 10 * SECOND)).toEqual({ fetch: true, reason: 'manual' });
  });

  it('fetches on pull to refresh even with fresh data', () => {
    expect(decideFetch(state({ manual: true }), t0 + 12 * SECOND)).toEqual({
      fetch: true,
      reason: 'manual',
    });
  });

  describe('after errors', () => {
    // Last success at t0; the request at t1 failed.
    const t1 = t0 + 40 * SECOND;
    const failed = (n: number, patch: Partial<FetchState> = {}) =>
      state({ lastAttemptAt: t1, consecutiveErrors: n, ...patch });

    it('backs off 5 s → 10 s → 20 s → 60 s, never under the 10 s floor', () => {
      expect(decideFetch(failed(1), t1 + 5 * SECOND)).toMatchObject({
        reason: 'minInterval',
        until: t1 + 10 * SECOND,
      });
      expect(decideFetch(failed(1), t1 + 10 * SECOND)).toEqual({ fetch: true, reason: 'retry' });
      expect(decideFetch(failed(2), t1 + 10 * SECOND)).toEqual({ fetch: true, reason: 'retry' });
      expect(decideFetch(failed(3), t1 + 10 * SECOND)).toEqual({
        fetch: false,
        reason: 'backoff',
        until: t1 + 20 * SECOND,
      });
      expect(decideFetch(failed(3), t1 + 20 * SECOND)).toEqual({ fetch: true, reason: 'retry' });
      expect(decideFetch(failed(7), t1 + 59 * SECOND)).toEqual({
        fetch: false,
        reason: 'backoff',
        until: t1 + MINUTE,
      });
      expect(decideFetch(failed(7), t1 + MINUTE)).toEqual({ fetch: true, reason: 'retry' });
    });

    it('does not let other triggers skip the backoff', () => {
      expect(decideFetch(failed(4, { position: far }), t1 + 30 * SECOND)).toMatchObject({
        fetch: false,
        reason: 'backoff',
      });
    });

    it('lets pull to refresh skip the backoff', () => {
      expect(decideFetch(failed(4, { manual: true }), t1 + 30 * SECOND)).toEqual({
        fetch: true,
        reason: 'manual',
      });
    });

    it('retries a failed first request', () => {
      const s = state({ last: null, lastAttemptAt: t0, consecutiveErrors: 1 });
      expect(decideFetch(s, t0 + 10 * SECOND)).toEqual({ fetch: true, reason: 'retry' });
    });

    it('does not back off without the time of the failed request', () => {
      const s = state({ last: null, lastAttemptAt: null, consecutiveErrors: 3 });
      expect(decideFetch(s, t0)).toEqual({ fetch: true, reason: 'initial' });
    });
  });
});

describe('nextRequestCenter', () => {
  it('starts at the position', () => {
    expect(nextRequestCenter(null, wedding)).toBe(wedding);
  });

  it('stays put for moves up to 150 m', () => {
    expect(nextRequestCenter(wedding, near)).toBe(wedding);
  });

  it('moves to the position after more than 150 m', () => {
    expect(nextRequestCenter(wedding, far)).toBe(far);
  });
});

describe('nearbyDepartureLists', () => {
  it('lists every direction of every pole of the transit layer', () => {
    const dep = (tripId: string) => ({
      tripId,
      serviceDate: '2026-11-05',
      scheduled: '2026-11-05T14:32:00+01:00',
      predicted: null,
      delaySeconds: null,
      source: 'scheduled' as const,
      cancelled: false,
    });
    const direction = (tripId: string) => ({
      line: 'M27',
      mode: 'bus' as const,
      color: null,
      headsign: 'S Jungfernheide',
      isTerminal: false,
      isRing: false,
      departures: [dep(tripId)],
    });
    const pole = (...dirs: ReturnType<typeof direction>[]) => ({
      id: 'p',
      location: { lat: 52.5, lng: 13.4 },
      platform: null,
      description: null,
      directions: dirs,
    });
    const station = (...poles: ReturnType<typeof pole>[]) => ({
      id: 's',
      kind: 'station' as const,
      name: 'S+U Wedding',
      location: { lat: 52.5, lng: 13.4 },
      modes: [],
      poles,
    });
    const meta = { generatedAt: '2026-11-05T14:30:12+01:00', attribution: [], layers: {} };
    const lists = nearbyDepartureLists({
      meta,
      layers: {
        transit: {
          layer: 'transit',
          features: [station(pole(direction('a'), direction('b'))), station(pole(direction('c')))],
        },
      },
    });
    expect([...lists].map((l) => l[0]!.tripId)).toEqual(['a', 'b', 'c']);
    expect([...nearbyDepartureLists({ meta, layers: {} })]).toEqual([]); // transit down
  });
});
