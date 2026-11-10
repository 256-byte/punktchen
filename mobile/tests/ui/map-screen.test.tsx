import { afterEach, beforeEach, describe, expect, it, jest } from '@jest/globals';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { createApiClient, type ApiClientOptions } from '@/api/client';
import type { NearbyResponse } from '@/api/nearby';
import { MIN_INTERVAL_MS } from '@/core/fetchPolicy';
import type { LatLng } from '@/core/geo';
import { regionAround } from '@/core/geo';
import { type Station, transitLayer } from '@/layers/transit';
import { createLocationStore, type LocationStore } from '@/state/location';
import { MapScreen, START_RADIUS_M } from '@/ui/MapScreen';

import { map } from './maps-mock';

jest.mock(
  'react-native-maps',
  () => jest.requireActual<typeof import('./maps-mock')>('./maps-mock').mapsMock,
);

const BASE = 'http://192.168.178.29:8080';
const WEDDING: LatLng = { lat: 52.5427, lng: 13.366 };
// 0.0001° latitude ≈ 11.1 m.
const north = (m: number): LatLng => ({
  lat: WEDDING.lat + (m / 11.12) * 0.0001,
  lng: WEDDING.lng,
});

type FakeFetch = NonNullable<ApiClientOptions['fetch']>;

function station(id: string, name: string) {
  return {
    id,
    kind: 'station' as const,
    name,
    location: { lat: 52.5427, lng: 13.366 },
    modes: ['bus' as const],
    poles: [
      {
        id: `${id}::1`,
        location: { lat: 52.5427, lng: 13.366 },
        platform: null,
        description: null,
        directions: [
          {
            line: 'M27',
            mode: 'bus' as const,
            color: null,
            headsign: `${name} → Jungfernheide`,
            isTerminal: false,
            isRing: false,
            // Markers show directions with an upcoming departure only.
            departures: [
              {
                tripId: '1',
                serviceDate: '2026-11-05',
                scheduled: new Date(Date.now() + 5 * 60_000).toISOString(),
                predicted: null,
                delaySeconds: null,
                source: 'scheduled' as const,
                cancelled: false,
              },
            ],
          },
        ],
      },
    ],
  };
}

function nearby(...stations: Station[]): NearbyResponse {
  return {
    meta: {
      generatedAt: '2026-11-05T14:30:12+01:00',
      attribution: ['Data: VBB (CC BY 4.0)'],
      layers: { transit: { version: 1, status: 'ok' } },
    },
    layers: { transit: { layer: 'transit', features: stations } },
  };
}

let store: LocationStore;
let requests: URL[];
let answer: () => Response;

const fakeFetch: FakeFetch = async (request) => {
  requests.push(new URL(request.url));
  return answer();
};

beforeEach(() => {
  store = createLocationStore();
  requests = [];
  answer = () =>
    new Response(JSON.stringify(nearby(station('de:11000:900009104', 'S+U Wedding'))), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    });
  map.props = null;
  map.animateToRegion.mockClear();
});

afterEach(() => {
  jest.useRealTimers();
});

async function renderMap(
  position: LatLng,
  source: 'live' | 'lastKnown' | 'mapCenter' = 'live',
  client = createApiClient(BASE, { fetch: fakeFetch }),
) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { gcTime: Infinity } } });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
  const ui = (p: LatLng, s: typeof source) => (
    <MapScreen
      position={p}
      source={s}
      baseUrl={{ ok: true, url: BASE }}
      client={client}
      store={store}
    />
  );
  const result = await render(ui(position, source), { wrapper });
  return {
    move: (p: LatLng, s: typeof source = source) => result.rerender(ui(p, s)),
  };
}

describe('MapScreen camera', () => {
  it('opens centred on the user (acceptance criterion)', async () => {
    await renderMap(WEDDING);
    expect(map.props?.initialRegion).toEqual(regionAround(WEDDING, START_RADIUS_M));
    expect(map.props?.showsUserLocation).toBe(true);
  });

  it('follows the device until the user pans the map', async () => {
    const { move } = await renderMap(WEDDING, 'lastKnown');
    expect(map.animateToRegion).toHaveBeenCalledTimes(1); // on open, to the same place

    await move(north(40), 'live');
    expect(map.animateToRegion).toHaveBeenLastCalledWith(
      regionAround(north(40), START_RADIUS_M),
      400,
    );

    await act(async () => map.props?.onPanDrag?.({} as never));
    map.animateToRegion.mockClear();
    await move(north(80));
    expect(map.animateToRegion).not.toHaveBeenCalled();
  });

  it('in map center mode does not move the camera, and reports where the map is', async () => {
    const { move } = await renderMap(WEDDING, 'mapCenter');
    expect(map.props?.showsUserLocation).toBe(false);
    await move(north(300));
    expect(map.animateToRegion).not.toHaveBeenCalled();

    await act(async () =>
      map.props?.onRegionChangeComplete?.(
        { latitude: 52.52, longitude: 13.41, latitudeDelta: 0.01, longitudeDelta: 0.02 },
        {},
      ),
    );
    expect(store.getState().mapCenter).toEqual({ lat: 52.52, lng: 13.41 });
  });
});

describe('MapScreen layers', () => {
  it('draws the poles from /v1/nearby through the layer registry', async () => {
    await renderMap(WEDDING);
    expect(await screen.findByText('S+U Wedding → Jungfernheide')).toBeOnTheScreen();
    expect(requests).toHaveLength(1);
    expect(requests[0]?.searchParams.get('lat')).toBe('52.5427');
  });

  it('shows the data attribution with the data (acceptance criterion 3.7)', async () => {
    await renderMap(WEDDING);
    expect(await screen.findByText('Data: VBB (CC BY 4.0)')).toBeOnTheScreen();
  });

  it('shows no attribution when there is nothing to attribute', async () => {
    answer = () =>
      new Response(JSON.stringify({ ...nearby(), meta: { ...nearby().meta, attribution: [] } }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    await renderMap(WEDDING);
    await waitFor(() => expect(requests).toHaveLength(1));
    expect(screen.queryByText(/CC BY/)).toBeNull();
  });

  it('requests again only after a move of more than 150 m, at most every 10 s', async () => {
    jest.useFakeTimers(); // also moves Date.now, the fetch policy's clock
    const { move } = await renderMap(WEDDING);
    await screen.findByText('S+U Wedding → Jungfernheide');

    await move(north(100));
    await move(north(140));
    expect(requests).toHaveLength(1);

    await move(north(200));
    expect(requests).toHaveLength(1); // within 10 s of the first request
    await act(async () => jest.advanceTimersByTime(MIN_INTERVAL_MS));
    await waitFor(() => expect(requests).toHaveLength(2));
    expect(Number(requests[1]?.searchParams.get('lat'))).toBeCloseTo(north(200).lat, 6);
  });

  it('explains a missing backend configuration', async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { gcTime: Infinity } } });
    await render(
      <QueryClientProvider client={queryClient}>
        <MapScreen
          position={WEDDING}
          source="live"
          baseUrl={{ ok: false, error: 'EXPO_PUBLIC_API_URL is not set' }}
          client={null}
          store={store}
        />
      </QueryClientProvider>,
    );
    expect(screen.getByText('Backend not configured')).toBeOnTheScreen();
    expect(map.props?.initialRegion).toEqual(regionAround(WEDDING, START_RADIUS_M));
  });

  it('shows a failed request and tries again', async () => {
    jest.useFakeTimers();
    answer = () => new Response('', { status: 503 });
    await renderMap(WEDDING);
    expect(await screen.findByText('Could not load departures')).toBeOnTheScreen();
    expect(screen.getByText('HTTP 503')).toBeOnTheScreen();

    answer = () =>
      new Response(JSON.stringify(nearby(station('de:11000:900009104', 'S+U Wedding'))), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    await fireEvent.press(screen.getByRole('button', { name: 'Try again' }));
    expect(requests).toHaveLength(1); // "Try again" skips the backoff, not the 10 s floor
    await act(async () => jest.advanceTimersByTime(MIN_INTERVAL_MS));
    expect(await screen.findByText('S+U Wedding → Jungfernheide')).toBeOnTheScreen();
    expect(requests).toHaveLength(2);
    expect(screen.queryByText('Could not load departures')).toBeNull();
  });

  it('clusters poles at default zoom and expands them on zoom-in (task 7.2)', async () => {
    const soon = new Date(Date.now() + 5 * 60_000).toISOString();
    const dep = {
      tripId: '1',
      serviceDate: '2026-11-05',
      scheduled: soon,
      predicted: null,
      delaySeconds: null,
      source: 'scheduled' as const,
      cancelled: false,
    };
    const alex = {
      id: 'de:11000:900100003',
      kind: 'station' as const,
      name: 'S+U Alexanderplatz',
      location: { lat: 52.52151, lng: 13.41127 },
      modes: ['ubahn' as const, 'bus' as const],
      poles: [
        {
          id: 'alex::bus',
          location: { lat: 52.5219, lng: 13.411 },
          platform: null,
          description: null,
          directions: [
            {
              line: '100',
              mode: 'bus' as const,
              color: null,
              headsign: 'S+U Zoologischer Garten',
              isTerminal: false,
              isRing: false,
              departures: [dep],
            },
          ],
        },
        {
          id: 'alex::u2',
          location: { lat: 52.5213, lng: 13.4114 }, // ~72 m from alex::bus
          platform: '1',
          description: null,
          directions: [
            {
              line: 'U2',
              mode: 'ubahn' as const,
              color: '#DA421E',
              headsign: 'S+U Pankow',
              isTerminal: false,
              isRing: false,
              departures: [dep],
            },
          ],
        },
      ],
    };
    answer = () =>
      new Response(JSON.stringify(nearby(alex)), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });

    await renderMap(alex.location);
    // Default zoom (latitudeDelta ≈ 0.0108 → stations): one clustered station marker.
    expect(await screen.findByText('S+U Alexanderplatz')).toBeOnTheScreen();
    expect(screen.getAllByTestId('marker')).toHaveLength(1);
    expect(screen.getByText('S+U Pankow')).toBeOnTheScreen();
    expect(screen.getByText('S+U Zoologischer Garten')).toBeOnTheScreen();

    // Zoom in to street level (latitudeDelta = 0.002 → poles): the two poles split.
    await act(async () =>
      map.props?.onRegionChangeComplete?.(
        {
          latitude: alex.location.lat,
          longitude: alex.location.lng,
          latitudeDelta: 0.002,
          longitudeDelta: 0.002,
        },
        {},
      ),
    );
    expect(screen.getAllByTestId('marker')).toHaveLength(2);

    // Zoom out to overview (latitudeDelta = 0.03 → compact): badges only, no headsigns.
    await act(async () =>
      map.props?.onRegionChangeComplete?.(
        {
          latitude: alex.location.lat,
          longitude: alex.location.lng,
          latitudeDelta: 0.03,
          longitudeDelta: 0.03,
        },
        {},
      ),
    );
    expect(screen.getAllByTestId('marker')).toHaveLength(1);
    expect(screen.getByText('U2')).toBeOnTheScreen();
    expect(screen.getByText('100')).toBeOnTheScreen();
    expect(screen.queryByText('S+U Pankow')).toBeNull();
  });
});

describe('transit layer', () => {
  it('selects the poles, and none when the layer is down', () => {
    const r = nearby(station('a', 'A'), station('b', 'B'));
    expect(transitLayer.select(r).map((s) => transitLayer.key(s))).toEqual(['a::1', 'b::1']);
    expect(transitLayer.select({ ...r, layers: {} })).toEqual([]);
  });
});
