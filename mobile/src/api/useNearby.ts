import { keepPreviousData, skipToken, useQuery, useQueryClient } from '@tanstack/react-query';

import { MAX_DATA_AGE_MS } from '@/core/fetchPolicy';

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

/**
 * Features around a point (GET /v1/nearby). Refetches revalidate with the
 * kept ETag; a 304 keeps the data and updates `checkedAt`. While new
 * parameters load, the previous area stays visible (`isPlaceholderData`).
 *
 * params null: no request (e.g. no position yet). When to refetch is the
 * fetch policy's job (ARCHITECTURE §4.2, wired in task 5.4): no retries here,
 * and the data counts as fresh for 30 s, so mounting another user of the
 * same query does not refetch.
 */
export function useNearby(client: ApiClient, params: NearbyParams | null) {
  const queryClient = useQueryClient();
  const queryKey = nearbyQueryKey(params);
  return useQuery({
    queryKey,
    queryFn:
      params === null
        ? skipToken
        : ({ signal }) =>
            fetchNearby(client, params, {
              previous: queryClient.getQueryData<Nearby>(queryKey),
              signal,
            }),
    placeholderData: keepPreviousData,
    retry: false,
    staleTime: MAX_DATA_AGE_MS,
  });
}
