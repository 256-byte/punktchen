import { afterEach, beforeEach, describe, expect, it, jest } from '@jest/globals';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react-native';

import { createApiClient, type ApiClientOptions } from '@/api/client';
import { MIN_INTERVAL_MS } from '@/core/fetchPolicy';
import { regionAround } from '@/core/geo';
import { createLocationStore, type LocationStore } from '@/state/location';
import { DEV_PLACES } from '@/ui/DevPlacePicker';
import { LocationGate } from '@/ui/LocationGate';
import { MapScreen, START_RADIUS_M } from '@/ui/MapScreen';

import { map } from './maps-mock';

jest.mock(
  'react-native-maps',
  () => jest.requireActual<typeof import('./maps-mock')>('./maps-mock').mapsMock,
);

const BASE = 'http://192.168.178.29:8080';
const HOME = { lat: 52.4472, lng: 13.2925 }; // somewhere in south-west Berlin
const ALEX = DEV_PLACES.find((p) => p.name === 'S+U Alexanderplatz')!.center;

let store: LocationStore;
let requests: URL[];

type FakeFetch = NonNullable<ApiClientOptions['fetch']>;
const fakeFetch: FakeFetch = async (request) => {
  requests.push(new URL(request.url));
  return new Response(
    JSON.stringify({
      meta: { generatedAt: '2026-11-05T14:30:12+01:00', attribution: [], layers: {} },
      layers: {},
    }),
    { status: 200, headers: { 'Content-Type': 'application/json' } },
  );
};

beforeEach(() => {
  store = createLocationStore();
  store.getState().setPermission({ status: 'granted', canAskAgain: true });
  store.getState().offerFix({ ...HOME, accuracy: 10, at: Date.now(), source: 'live' });
  requests = [];
  map.animateToRegion.mockClear();
});

async function renderApp(devTools: boolean) {
  const client = createApiClient(BASE, { fetch: fakeFetch });
  await render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { gcTime: Infinity } } })}
    >
      <LocationGate store={store}>
        {({ position, source }) => (
          <MapScreen
            position={position}
            source={source}
            baseUrl={{ ok: true, url: BASE }}
            client={client}
            store={store}
            devTools={devTools}
          />
        )}
      </LocationGate>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  jest.useRealTimers();
});

describe('DevPlacePicker', () => {
  it('jumps to a station and loads its surroundings', async () => {
    jest.useFakeTimers(); // the new area loads after the fetch policy's 10 s floor
    await renderApp(true);
    await waitFor(() => expect(requests).toHaveLength(1));

    await fireEvent.press(screen.getByRole('button', { name: 'Dev: place' }));
    for (const p of DEV_PLACES) expect(screen.getByText(p.name)).toBeOnTheScreen();
    await fireEvent.press(screen.getByRole('button', { name: 'S+U Alexanderplatz' }));

    expect(store.getState().mode).toBe('mapCenter');
    expect(map.animateToRegion).toHaveBeenLastCalledWith(regionAround(ALEX, START_RADIUS_M), 400);
    await act(async () => jest.advanceTimersByTime(MIN_INTERVAL_MS));
    await waitFor(() => expect(requests).toHaveLength(2));
    expect(Number(requests[1]?.searchParams.get('lat'))).toBeCloseTo(ALEX.lat, 5);
    expect(screen.queryByText('S Wannsee')).toBeNull(); // list closed
  });

  it('goes back to the device position and follows it again', async () => {
    await renderApp(true);
    await fireEvent.press(screen.getByRole('button', { name: 'Dev: place' }));
    await fireEvent.press(screen.getByRole('button', { name: 'S Wannsee' }));
    map.animateToRegion.mockClear();

    await fireEvent.press(screen.getByRole('button', { name: 'Dev: place' }));
    await fireEvent.press(screen.getByRole('button', { name: 'My location' }));

    expect(store.getState().mode).toBe('device');
    await waitFor(() =>
      expect(map.animateToRegion).toHaveBeenLastCalledWith(regionAround(HOME, START_RADIUS_M), 400),
    );
  });

  it('is not shown outside development', async () => {
    await renderApp(false);
    expect(screen.queryByRole('button', { name: 'Dev: place' })).toBeNull();
  });
});
