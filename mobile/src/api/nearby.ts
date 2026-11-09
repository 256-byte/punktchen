// GET /v1/nearby with ETag revalidation (src/api/revalidate.ts).
import type { ApiClient } from './client';
import { conditionalHeader, type Kept, keepAnswer, type RevalidateOptions } from './revalidate';
import type { components, operations } from './schema.gen';

export type NearbyResponse = components['schemas']['NearbyResponse'];
export type NearbyParams = operations['getNearby']['parameters']['query'];

/** One /v1/nearby answer as the app keeps it. */
export type Nearby = Kept<NearbyResponse>;

export type FetchNearbyOptions = RevalidateOptions<NearbyResponse>;

export async function fetchNearby(
  client: ApiClient,
  params: NearbyParams,
  { previous, signal, now = Date.now }: FetchNearbyOptions = {},
): Promise<Nearby> {
  const result = await client.GET('/v1/nearby', {
    params: { query: params, header: conditionalHeader(previous) },
    signal,
  });
  return keepAnswer(result, previous, now());
}
