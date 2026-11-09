// GET /v1/stations/{stationId} (station sheet, task 3.6) with ETag
// revalidation (src/api/revalidate.ts).
import { keepPreviousData, skipToken, useQuery, useQueryClient } from '@tanstack/react-query';

import { MAX_DATA_AGE_MS } from '@/core/fetchPolicy';

import type { ApiClient } from './client';
import { conditionalHeader, type Kept, keepAnswer, type RevalidateOptions } from './revalidate';
import type { components } from './schema.gen';

export type StationResponse = components['schemas']['StationResponse'];

/** One /v1/stations/{id} answer as the app keeps it. */
export type StationAnswer = Kept<StationResponse>;

export async function fetchStation(
  client: ApiClient,
  stationId: string,
  { previous, signal, now = Date.now }: RevalidateOptions<StationResponse> = {},
): Promise<StationAnswer> {
  const result = await client.GET('/v1/stations/{stationId}', {
    params: { path: { stationId }, header: conditionalHeader(previous) },
    signal,
  });
  return keepAnswer(result, previous, now());
}

export function stationQueryKey(stationId: string | null) {
  return ['station', stationId] as const;
}

/**
 * One station with all its poles (up to 10 departures per direction). Same
 * rules as useNearby: revalidates with the kept ETag, no retries, fresh for
 * 30 s, refetched every 30 s until the fetch policy is wired (5.4).
 * client or stationId null: no request.
 */
export function useStation(client: ApiClient | null, stationId: string | null) {
  const queryClient = useQueryClient();
  const queryKey = stationQueryKey(stationId);
  return useQuery({
    queryKey,
    queryFn:
      client === null || stationId === null
        ? skipToken
        : ({ signal }) =>
            fetchStation(client, stationId, {
              previous: queryClient.getQueryData<StationAnswer>(queryKey),
              signal,
            }),
    placeholderData: keepPreviousData,
    retry: false,
    staleTime: MAX_DATA_AGE_MS,
    refetchInterval: MAX_DATA_AGE_MS,
  });
}
