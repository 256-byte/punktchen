// GET /v1/nearby with ETag revalidation (DATA_MODEL §5.4): the previous
// answer's ETag goes out as If-None-Match; a 304 keeps the previous data and
// only moves its "last checked" time.
import { clockOffset } from '@/core/time';

import { ApiError, type ApiClient } from './client';
import type { components, operations } from './schema.gen';

export type NearbyResponse = components['schemas']['NearbyResponse'];
export type NearbyParams = operations['getNearby']['parameters']['query'];

/** One /v1/nearby answer as the app keeps it. */
export interface Nearby {
  response: NearbyResponse;
  /** ETag of the response; null if the server sent none. */
  etag: string | null;
  /** Device time of the last 200 or 304 (data age, ARCHITECTURE §4.3). */
  checkedAt: number;
  /** Add to the device clock to get server time (ADR-0007); from the last 200. */
  clockOffsetMs: number;
}

export interface FetchNearbyOptions {
  /** The data kept for these parameters, if any: its ETag is revalidated. */
  previous?: Nearby;
  signal?: AbortSignal;
  /** Device clock (tests). */
  now?: () => number;
}

export async function fetchNearby(
  client: ApiClient,
  params: NearbyParams,
  { previous, signal, now = Date.now }: FetchNearbyOptions = {},
): Promise<Nearby> {
  const etag = previous?.etag ?? null;
  const { data, error, response } = await client.GET('/v1/nearby', {
    params: { query: params, header: etag === null ? {} : { 'If-None-Match': etag } },
    signal,
  });
  const checkedAt = now();
  const status: number = response.status;

  if (status === 304) {
    if (previous === undefined) {
      throw new ApiError('304 Not Modified without a previous response', 304);
    }
    return { ...previous, etag: response.headers.get('ETag') ?? previous.etag, checkedAt };
  }
  if (data) {
    return {
      response: data,
      etag: response.headers.get('ETag'),
      checkedAt,
      clockOffsetMs: clockOffset(data.meta.generatedAt, checkedAt),
    };
  }
  const message =
    typeof error === 'object' && error !== null ? error.error.message : `HTTP ${status}`;
  throw new ApiError(message, status);
}
