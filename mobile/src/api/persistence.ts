// What of the query cache is kept on the device (task 6.1, ARCHITECTURE
// §4.3): the newest /v1/nearby answer, so a cold start shows the last data
// at once. The AsyncStorage glue is in src/lib/queryPersister.ts.
import type { Query, QueryClient } from '@tanstack/react-query';
import type {
  PersistedClient,
  Persister,
  PersistQueryClientOptions,
} from '@tanstack/react-query-persist-client';

import type { LatLng } from '@/core/geo';

/** Kept data older than this is dropped on start (its departures have long left). */
export const PERSIST_MAX_AGE_MS = 24 * 60 * 60 * 1000;
/** Bump when the kept shape changes (Kept<T>, the API schema): older data is dropped. */
export const PERSIST_BUSTER = 'v1';
/** The AsyncStorage key of the kept cache. */
export const PERSIST_KEY = 'livestop.query-cache';

/** The /v1/nearby query whose data is the newest; undefined without any. */
export function lastNearbyQuery(client: QueryClient): Query | undefined {
  let newest: Query | undefined;
  for (const q of client.getQueryCache().findAll({ queryKey: ['nearby'] })) {
    if (
      q.state.data !== undefined &&
      (newest === undefined || q.state.dataUpdatedAt > newest.state.dataUpdatedAt)
    ) {
      newest = q;
    }
  }
  return newest;
}

/**
 * Where the newest /v1/nearby answer was requested for; null without one.
 * After a cold start the map opens there until the device has a position.
 */
export function lastNearbyCenter(client: QueryClient): LatLng | null {
  const params = lastNearbyQuery(client)?.queryKey[1] as Partial<LatLng> | null | undefined;
  return typeof params?.lat === 'number' && typeof params.lng === 'number'
    ? { lat: params.lat, lng: params.lng }
    : null;
}

/**
 * Only the newest /v1/nearby answer is kept: one entry of 30–75 KB instead
 * of one per place visited. It is kept whatever its status: a failed
 * refetch (offline) must not drop the last good data from the device.
 */
export function shouldPersist(client: QueryClient): (query: Query) => boolean {
  return (query) => query === lastNearbyQuery(client);
}

/**
 * The kept cache as a string. Queries are written as successful and idle:
 * the error of a failed refetch belongs to the old session (and an Error
 * does not survive JSON anyway).
 */
export function serializeClient(persisted: PersistedClient): string {
  const queries = persisted.clientState.queries.map((q) => ({
    ...q,
    state: {
      ...q.state,
      status: 'success' as const,
      fetchStatus: 'idle' as const,
      error: null,
      errorUpdateCount: 0,
      errorUpdatedAt: 0,
      fetchFailureCount: 0,
      fetchFailureReason: null,
      isInvalidated: false,
    },
  }));
  return JSON.stringify({
    ...persisted,
    clientState: { ...persisted.clientState, queries, mutations: [] },
  });
}

/** Options of PersistQueryClientProvider for this client. */
export function persistOptions(
  client: QueryClient,
  persister: Persister,
): Omit<PersistQueryClientOptions, 'queryClient'> {
  return {
    persister,
    maxAge: PERSIST_MAX_AGE_MS,
    buster: PERSIST_BUSTER,
    dehydrateOptions: {
      shouldDehydrateQuery: shouldPersist(client),
      shouldDehydrateMutation: () => false,
    },
  };
}
