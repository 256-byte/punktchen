import { keepPreviousData, skipToken, useQuery, useQueryClient } from '@tanstack/react-query';
import { useCallback, useEffect, useEffectEvent, useRef, useState } from 'react';
import { AppState } from 'react-native';

import { earliestDepartureAt } from '@/core/departures';
import { createFetchLoop, type FetchLoop, type FetchResult } from '@/core/fetchLoop';
import { nearbyDepartureLists } from '@/core/fetchPolicy';
import type { LatLng } from '@/core/geo';

import type { ApiClient } from './client';
import { fetchNearby, type Nearby, type NearbyParams } from './nearby';

/**
 * Query key of /v1/nearby. Defaults are filled in, so the same request
 * written two ways shares one cache entry (and its ETag).
 */
export function nearbyQueryKey(params: NearbyParams | null) {
  if (params === null) {
    return ['nearby', null] as const;
  }
  const { lat, lng, radius = 500, layers = ['transit'] } = params;
  return ['nearby', { lat, lng, radius, layers: [...layers].sort() }] as const;
}

export interface UseNearbyOptions {
  /**
   * Device clock (epoch ms) for the policy's intervals. Not the
   * server-corrected one: the correction changes with a response, and
   * "10 s since the last request" must not jump with it.
   */
  now?: () => number;
  /** Development log of requests (default: console in dev builds, none in tests). */
  log?: (line: string) => void;
}

const devLog =
  __DEV__ && process.env.NODE_ENV !== 'test' ? (line: string) => console.log(line) : undefined;

/**
 * Features around a point (GET /v1/nearby). Requests revalidate with the
 * kept ETag; a 304 keeps the data and updates `checkedAt`. While new
 * parameters load, the previous area stays visible (`isPlaceholderData`).
 *
 * When to request is the fetch policy's job (ARCHITECTURE §4.2, task 5.4):
 * only in the foreground, on the first data, a move of > 150 m (params
 * change), a passed earliest departure, data 30 s old or `refresh()`; never
 * within 10 s of the last request; backoff after errors. The query itself
 * never fetches on its own (`enabled: false`, no retries).
 *
 * client or params null: no request (backend not configured, no position
 * yet).
 */
export function useNearby(
  client: ApiClient | null,
  params: NearbyParams | null,
  { now = () => Date.now(), log = devLog }: UseNearbyOptions = {},
) {
  const queryClient = useQueryClient();
  const queryKey = nearbyQueryKey(params);
  const request =
    client === null || params === null
      ? null
      : ({ signal }: { signal: AbortSignal }) =>
          fetchNearby(client, params, {
            previous: queryClient.getQueryData<Nearby>(queryKey),
            signal,
          });
  const query = useQuery({
    queryKey,
    queryFn: request ?? skipToken,
    placeholderData: keepPreviousData,
    retry: false,
    enabled: false,
  });

  const position: LatLng | null =
    client !== null && params !== null ? { lat: params.lat, lng: params.lng } : null;

  // The loop reaches the latest key, clock and log through effect events.
  // It requests with fetchQuery, not query.refetch: reading a property of
  // the result during render makes TanStack Query re-render only when that
  // property changes (tracked properties).
  const fetchCurrent = useEffectEvent(async (): Promise<FetchResult> => {
    if (request === null) {
      return { ok: false, error: 'no backend or position' }; // the loop has no position then
    }
    // Updates the same cache entry the hook shows (errors included).
    const data = await queryClient.fetchQuery({
      queryKey,
      queryFn: request,
      staleTime: 0,
      retry: false,
    });
    // Departures are in server time: compare with the device clock plus this
    // answer's correction, and hand the result back in device time.
    const offset = data.clockOffsetMs;
    const earliest = earliestDepartureAt(nearbyDepartureLists(data.response), now() + offset);
    return { ok: true, earliestDepartureAt: earliest === null ? null : earliest - offset };
  });
  const deviceNow = useEffectEvent(() => now());
  const writeLog = useEffectEvent((line: string) => log?.(line));
  const currentPosition = useEffectEvent(() => position);

  const loop = useRef<FetchLoop | null>(null);
  // One loop per mount, created and stopped in the same effect (safe when
  // effects run twice). Foreground from AppState: timers do not run in the
  // background; on return the loop decides at once (e.g. data 30 s old).
  useEffect(() => {
    const l = createFetchLoop({
      now: () => deviceNow(),
      fetch: () => fetchCurrent(),
      setTimer: (run, ms) => setTimeout(run, ms),
      clearTimer: (t) => clearTimeout(t as ReturnType<typeof setTimeout>),
      log: (line) => writeLog(line),
    });
    loop.current = l;
    l.update({ foreground: AppState.currentState !== 'background', position: currentPosition() });
    const sub = AppState.addEventListener('change', (state) =>
      l.update({ foreground: state !== 'background' }),
    );
    return () => {
      sub.remove();
      l.dispose();
      loop.current = null;
    };
  }, []);

  const lat = position?.lat;
  const lng = position?.lng;
  useEffect(() => {
    loop.current?.update({ position: lat === undefined ? null : { lat, lng: lng! } });
  }, [lat, lng]);

  // refresh() only counts; the effect hands it to the loop.
  const [refreshes, setRefreshes] = useState(0);
  useEffect(() => {
    if (refreshes > 0) loop.current?.refresh();
  }, [refreshes]);
  const refresh = useCallback(() => setRefreshes((n) => n + 1), []);

  // Added to TanStack's result object rather than spread into a copy: a
  // spread would read (and so subscribe to) every tracked property.
  return Object.assign(query, { refresh });
}
