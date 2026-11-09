import { describe, expect, it } from 'vitest';

import { createFetchLoop, type FetchResult } from './fetchLoop';
import { MAX_DATA_AGE_MS, MIN_INTERVAL_MS } from './fetchPolicy';
import type { LatLng } from './geo';
import { MINUTE, SECOND } from './time';

const WEDDING: LatLng = { lat: 52.5427, lng: 13.366 };
const north = (m: number): LatLng => ({ lat: WEDDING.lat + m / 111_195, lng: WEDDING.lng });

/** A loop on a hand-moved clock; requests stay open until answered. */
function setup(t0 = 1_000_000) {
  let now = t0;
  let timers: { at: number; run: () => void; id: number }[] = [];
  let nextId = 0;
  const requests: { reason: string; at: number; answer: (r: FetchResult | Error) => void }[] = [];
  const log: string[] = [];
  const loop = createFetchLoop({
    now: () => now,
    fetch: (reason) =>
      new Promise((resolve, reject) => {
        requests.push({
          reason,
          at: now,
          answer: (r) => (r instanceof Error ? reject(r) : resolve(r)),
        });
      }),
    setTimer: (run, ms) => {
      const id = nextId++;
      timers.push({ at: now + ms, run, id });
      return id;
    },
    clearTimer: (id) => {
      timers = timers.filter((t) => t.id !== id);
    },
    log: (line) => log.push(line),
  });
  /** Moves the clock, running timers that come due. */
  const advance = async (ms: number) => {
    const end = now + ms;
    for (;;) {
      timers.sort((a, b) => a.at - b.at);
      const t = timers[0];
      if (!t || t.at > end) break;
      timers.shift();
      now = t.at;
      t.run();
      await flush();
    }
    now = end;
  };
  const answer = async (r: FetchResult | Error) => {
    requests[requests.length - 1]!.answer(r);
    await flush();
  };
  return { loop, requests, log, advance, answer, timers: () => timers, now: () => now };
}

const flush = () => new Promise<void>((r) => setTimeout(r, 0));
const ok = (earliestDepartureAt: number | null = null): FetchResult => ({
  ok: true,
  earliestDepartureAt,
});

describe('createFetchLoop', () => {
  it('fetches once there is a position in the foreground', async () => {
    const t = setup();
    t.loop.update({ position: WEDDING });
    expect(t.requests).toHaveLength(0); // still in the background
    t.loop.update({ foreground: true, position: null });
    expect(t.requests).toHaveLength(0); // no position
    t.loop.update({ position: WEDDING });
    expect(t.requests.map((r) => r.reason)).toEqual(['initial']);
  });

  it('refetches when the data is 30 s old, with one timer', async () => {
    const t = setup();
    t.loop.update({ foreground: true, position: WEDDING });
    await t.answer(ok());
    expect(t.timers()).toHaveLength(1);
    await t.advance(MAX_DATA_AGE_MS - 1);
    expect(t.requests).toHaveLength(1);
    await t.advance(1);
    expect(t.requests.map((r) => r.reason)).toEqual(['initial', 'age']);
  });

  it('refetches a minute after the earliest departure, if that comes first', async () => {
    const t = setup();
    t.loop.update({ foreground: true, position: WEDDING });
    await t.answer(ok(t.now() - 50 * SECOND)); // left 50 s ago: gone in 10 s
    await t.advance(MIN_INTERVAL_MS);
    expect(t.requests.map((r) => r.reason)).toEqual(['initial', 'departed']);
  });

  it('waits for the 10 s floor after a move, then fetches for the new place', async () => {
    const t = setup();
    t.loop.update({ foreground: true, position: WEDDING });
    await t.answer(ok());
    await t.advance(3 * SECOND);
    t.loop.update({ position: north(100) });
    t.loop.update({ position: north(200) });
    expect(t.requests).toHaveLength(1);
    await t.advance(MIN_INTERVAL_MS - 3 * SECOND);
    expect(t.requests.map((r) => r.reason)).toEqual(['initial', 'moved']);
    expect(t.requests[1]!.at - t.requests[0]!.at).toBe(MIN_INTERVAL_MS);
    await t.answer(ok());
    expect(t.loop.state().last?.position).toEqual(north(200));
  });

  it('backs off after errors and resets on success', async () => {
    const t = setup();
    t.loop.update({ foreground: true, position: WEDDING });
    await t.answer({ ok: false, error: 'HTTP 503' });
    await t.advance(MIN_INTERVAL_MS); // 1st backoff 5 s, but the floor is 10 s
    expect(t.requests.map((r) => r.reason)).toEqual(['initial', 'retry']);
    await t.answer(new Error('Network request failed')); // thrown: a failure too
    await t.advance(10 * SECOND);
    expect(t.requests).toHaveLength(3);
    await t.answer({ ok: false });
    await t.advance(20 * SECOND - 1);
    expect(t.requests).toHaveLength(3);
    await t.advance(1);
    expect(t.requests).toHaveLength(4);
    expect(t.loop.state().consecutiveErrors).toBe(3);
    await t.answer(ok());
    expect(t.loop.state().consecutiveErrors).toBe(0);
    expect(t.log).toContain('[nearby] failed (1 in a row): HTTP 503');
    expect(t.log).toContain('[nearby] failed (2 in a row): Network request failed');
    expect(t.log).toContain('[nearby] failed (3 in a row): unknown error');
  });

  it('"Try again" skips the backoff but not the floor', async () => {
    const t = setup();
    t.loop.update({ foreground: true, position: WEDDING });
    await t.answer({ ok: false });
    await t.advance(MIN_INTERVAL_MS);
    await t.answer({ ok: false });
    await t.advance(2 * SECOND); // backoff 10 s
    t.loop.refresh();
    expect(t.requests).toHaveLength(2);
    await t.advance(MIN_INTERVAL_MS - 2 * SECOND);
    expect(t.requests.map((r) => r.reason)).toEqual(['initial', 'retry', 'manual']);
  });

  it('runs one request at a time', async () => {
    const t = setup();
    t.loop.update({ foreground: true, position: WEDDING });
    await t.advance(MIN_INTERVAL_MS + 1);
    t.loop.update({ position: north(500) });
    t.loop.refresh();
    expect(t.requests).toHaveLength(1);
    await t.answer(ok()); // the end decides: moved, after the floor (passed already)
    expect(t.requests.map((r) => r.reason)).toEqual(['initial', 'manual']);
  });

  it('stops in the background and decides at once on return', async () => {
    const t = setup();
    t.loop.update({ foreground: true, position: WEDDING });
    await t.answer(ok());
    t.loop.update({ foreground: false });
    expect(t.timers()).toHaveLength(0);
    await t.advance(5 * MINUTE);
    expect(t.requests).toHaveLength(1);
    t.loop.update({ foreground: true });
    expect(t.requests.map((r) => r.reason)).toEqual(['initial', 'age']);
  });

  it('counts the requests of the last minute in the log', async () => {
    const t = setup();
    t.loop.update({ foreground: true, position: WEDDING });
    for (let i = 0; i < 3; i++) {
      await t.answer(ok());
      await t.advance(MAX_DATA_AGE_MS);
    }
    expect(t.log.filter((l) => l.includes('request'))).toEqual([
      '[nearby] request (initial), 1 in the last minute',
      '[nearby] request (age), 2 in the last minute',
      '[nearby] request (age), 2 in the last minute', // the first is a minute old
      '[nearby] request (age), 2 in the last minute',
    ]);
    expect(t.loop.requestsLastMinute()).toBe(2);
  });

  it('ignores a request that ends after dispose', async () => {
    const t = setup();
    t.loop.update({ foreground: true, position: WEDDING });
    t.loop.dispose();
    await t.answer(ok());
    expect(t.loop.state().last).toBeNull();
    expect(t.timers()).toHaveLength(0);
    t.loop.update({ position: north(500) });
    await t.advance(MINUTE);
    expect(t.requests).toHaveLength(1);
  });

  it('clears its timer on dispose', async () => {
    const t = setup();
    t.loop.update({ foreground: true, position: WEDDING });
    await t.answer(ok());
    expect(t.timers()).toHaveLength(1);
    t.loop.dispose();
    expect(t.timers()).toHaveLength(0);
  });

  it('ignores updates that change nothing', async () => {
    const t = setup();
    t.loop.update({ foreground: true, position: WEDDING });
    await t.answer(ok());
    const timer = t.timers()[0];
    t.loop.update({ foreground: true, position: { ...WEDDING } });
    expect(t.timers()[0]).toBe(timer);
  });
});
