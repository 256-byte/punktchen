import AsyncStorage from '@react-native-async-storage/async-storage';
import { beforeEach, describe, expect, it, jest } from '@jest/globals';
import { dehydrate, QueryClient } from '@tanstack/react-query';
import { PersistQueryClientProvider } from '@tanstack/react-query-persist-client';
import { act, render, screen, waitFor } from '@testing-library/react-native';

import type { Nearby, NearbyResponse } from '@/api/nearby';
import {
  lastNearbyCenter,
  lastNearbyQuery,
  PERSIST_KEY,
  persistOptions,
  serializeClient,
  shouldPersist,
} from '@/api/persistence';
import { nearbyQueryKey } from '@/api/useNearby';
import { regionAround } from '@/core/geo';
import { queryPersister } from '@/lib/queryPersister';
import { locationStore } from '@/state/location';
import { START_RADIUS_M } from '@/ui/MapScreen';

import HomeScreen from '../../app/index';
import { fake, position } from './location-mock';
import { map } from './maps-mock';

jest.mock(
  'expo-location',
  () => jest.requireActual<typeof import('./location-mock')>('./location-mock').expoLocationMock,
);
jest.mock(
  'react-native-maps',
  () => jest.requireActual<typeof import('./maps-mock')>('./maps-mock').mapsMock,
);
// The app's client, with the test's server behind it.
jest.mock('@/lib/api', () => {
  const { createApiClient } = jest.requireActual<typeof import('@/api/client')>('@/api/client');
  const url = 'http://192.168.178.29:8080';
  return {
    apiBaseUrl: { ok: true, url },
    apiClient: createApiClient(url, { fetch: (request: Request) => mockServer.fetch(request) }),
  };
});

const WEDDING = { lat: 52.5427, lng: 13.366 };
const NORTH = { lat: 52.546, lng: 13.366 }; // ~370 m north

function nearby(): NearbyResponse {
  return {
    meta: {
      generatedAt: new Date().toISOString(),
      attribution: ['Data: VBB (CC BY 4.0)'],
      layers: { transit: { version: 1, status: 'ok' } },
    },
    layers: {
      transit: {
        layer: 'transit',
        features: [
          {
            id: 'de:11000:900009104',
            kind: 'station',
            name: 'S+U Wedding',
            location: WEDDING,
            modes: ['bus'],
            poles: [
              {
                id: 'de:11000:900009104::1',
                location: WEDDING,
                platform: null,
                description: null,
                directions: [
                  {
                    line: 'M27',
                    mode: 'bus',
                    color: null,
                    headsign: 'S Jungfernheide',
                    isTerminal: false,
                    isRing: false,
                    departures: [
                      {
                        tripId: '1',
                        serviceDate: '2026-11-05',
                        scheduled: new Date(Date.now() + 20 * 60_000).toISOString(),
                        predicted: null,
                        delaySeconds: null,
                        source: 'scheduled',
                        cancelled: false,
                      },
                    ],
                  },
                ],
              },
            ],
          },
        ],
      },
    },
  };
}

const ok = () =>
  new Response(JSON.stringify(nearby()), {
    status: 200,
    headers: { 'Content-Type': 'application/json', ETag: 'W/"v1"' },
  });

const mockServer = {
  requests: [] as Request[],
  answer: (): Response | Promise<Response> => ok(),
  async fetch(request: Request) {
    this.requests.push(request);
    return this.answer();
  },
};

const newClient = () => new QueryClient({ defaultOptions: { queries: { gcTime: Infinity } } });

/** One run of the app, from start to quit. */
async function startApp() {
  const client = newClient();
  return render(
    <PersistQueryClientProvider
      client={client}
      persistOptions={persistOptions(client, queryPersister)}
    >
      <HomeScreen />
    </PersistQueryClientProvider>,
  );
}

const stored = async () => (await AsyncStorage.getItem(PERSIST_KEY)) ?? '';

/** In a function, so TypeScript does not narrow map.props to null afterwards. */
function resetMap() {
  map.props = null;
}

beforeEach(async () => {
  await AsyncStorage.clear();
  fake.reset();
  fake.current = { status: 'granted', canAskAgain: true };
  locationStore.setState({
    permission: { status: 'unknown', canAskAgain: true },
    fix: null,
    mode: 'device',
  });
  mockServer.requests = [];
  mockServer.answer = () => ok();
  resetMap();
  map.animateToRegion.mockClear();
});

describe('cold start (task 6.1)', () => {
  it('shows the last data at once, without a spinner, then revalidates', async () => {
    // First run: a position, an answer; it is written to the device.
    fake.lastKnown = position(WEDDING.lat, WEDDING.lng, Date.now());
    const first = await startApp();
    expect(await screen.findByText('S Jungfernheide')).toBeOnTheScreen();
    await waitFor(async () => expect(await stored()).toContain('S Jungfernheide'), {
      timeout: 3000,
    });
    await first.unmount();

    // Second run: no position yet, and the server does not answer yet.
    fake.lastKnown = null;
    locationStore.setState({ permission: { status: 'unknown', canAskAgain: true }, fix: null });
    let release: (r: Response) => void = () => {};
    mockServer.requests = [];
    mockServer.answer = () => new Promise<Response>((resolve) => (release = resolve));
    resetMap();
    await startApp();

    expect(await screen.findByText('S Jungfernheide')).toBeOnTheScreen();
    expect(screen.queryByText('Finding your location…')).toBeNull();
    expect(map.props?.initialRegion).toEqual(regionAround(WEDDING, START_RADIUS_M));
    // The kept answer is revalidated at once, with its ETag.
    await waitFor(() => expect(mockServer.requests).toHaveLength(1));
    expect(mockServer.requests[0]!.headers.get('If-None-Match')).toBe('W/"v1"');
    expect(new URL(mockServer.requests[0]!.url).searchParams.get('lat')).toBe(String(WEDDING.lat));

    await act(async () =>
      release(new Response(null, { status: 304, headers: { ETag: 'W/"v1"' } })),
    );
    expect(screen.getByText('S Jungfernheide')).toBeOnTheScreen();

    // The device's position arrives: the map goes there.
    await act(async () => fake.emit(position(NORTH.lat, NORTH.lng, Date.now())));
    expect(map.animateToRegion).toHaveBeenLastCalledWith(regionAround(NORTH, START_RADIUS_M), 400);
  });

  it('waits for the location on the very first start', async () => {
    await startApp();
    expect(await screen.findByText('Finding your location…')).toBeOnTheScreen();
    expect(mockServer.requests).toHaveLength(0);
  });
});

describe('what is kept', () => {
  const kept = (etag: string): Nearby => ({
    response: nearby(),
    etag,
    checkedAt: 1,
    clockOffsetMs: 0,
  });

  it('keeps only the newest /v1/nearby answer', () => {
    const client = newClient();
    const alex = { lat: 52.5219, lng: 13.4132 };
    client.setQueryData(nearbyQueryKey(WEDDING), kept('"old"'), { updatedAt: 1_000 });
    client.setQueryData(nearbyQueryKey(alex), kept('"new"'), { updatedAt: 2_000 });
    client.setQueryData(['station', 'de:11000:900009104'], { any: 'thing' }, { updatedAt: 3_000 });
    client.setQueryData(nearbyQueryKey(null), undefined);

    expect(lastNearbyQuery(client)?.queryKey).toEqual(nearbyQueryKey(alex));
    expect(lastNearbyCenter(client)).toEqual(alex);
    const state = dehydrate(client, { shouldDehydrateQuery: shouldPersist(client) });
    expect(state.queries.map((q) => q.queryKey)).toEqual([nearbyQueryKey(alex)]);

    expect(lastNearbyCenter(newClient())).toBeNull();
  });

  it('keeps the last good answer after a failed refetch, written as successful', async () => {
    const client = newClient();
    const key = nearbyQueryKey(WEDDING);
    client.setQueryData(key, kept('"v1"'));
    await client
      .fetchQuery({
        queryKey: key,
        queryFn: () => Promise.reject(new Error('offline')),
        retry: false,
      })
      .catch(() => {});
    expect(client.getQueryState(key)?.status).toBe('error');

    const state = dehydrate(client, { shouldDehydrateQuery: shouldPersist(client) });
    expect(state.queries).toHaveLength(1);
    const written = JSON.parse(
      serializeClient({ timestamp: 5, buster: 'v1', clientState: state }),
    ) as { timestamp: number; clientState: { queries: { state: Record<string, unknown> }[] } };
    expect(written.timestamp).toBe(5);
    expect(written.clientState.queries[0]!.state).toMatchObject({
      status: 'success',
      fetchStatus: 'idle',
      error: null,
      fetchFailureCount: 0,
      data: { etag: '"v1"' },
    });
  });
});
