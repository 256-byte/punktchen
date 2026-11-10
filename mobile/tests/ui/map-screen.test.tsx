import { afterEach, beforeEach, describe, expect, it, jest } from '@jest/globals';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { createApiClient, type ApiClientOptions } from '@/api/client';
import type { NearbyResponse } from '@/api/nearby';
import { MIN_INTERVAL_MS } from '@/core/fetchPolicy';
import type { LatLng } from '@/core/geo';
import { MAX_3D_ALTITUDE_M, MIN_3D_ZOOM, PITCH_3D_DEG, regionAround } from '@/core/geo';
import { type Station, transitLayer } from '@/layers/transit';
import * as haptics from '@/lib/haptics';
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
  map.scale = 100_000;
  map.animateToRegion.mockClear();
  map.animateCamera.mockClear();
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

  it('enables 3D buildings and toggles 3D camera pitch on iOS and Android (task 7.4)', async () => {
    const { move } = await renderMap(WEDDING, 'lastKnown');
    expect(map.props?.showsBuildings).toBe(true);
    expect(map.props?.pitchEnabled).toBe(true);

    const toggle = screen.getByRole('button', { name: '3D' });
    expect(toggle.props.accessibilityState).toEqual({ selected: false });

    // Enable 3D: tilts to 50° and brings altitude (iOS) and zoom (Android) into 3D building range.
    await fireEvent.press(toggle);
    expect(toggle.props.accessibilityState).toEqual({ selected: true });
    expect(map.animateCamera).toHaveBeenLastCalledWith(
      {
        pitch: PITCH_3D_DEG,
        heading: 0,
        altitude: MAX_3D_ALTITUDE_M,
        zoom: MIN_3D_ZOOM,
      },
      { duration: 400 },
    );

    // Moving or jumping while in 3D keeps the 3D camera instead of flattening with animateToRegion.
    map.animateToRegion.mockClear();
    map.animateCamera.mockClear();
    await move(north(40), 'live');
    expect(map.animateToRegion).not.toHaveBeenCalled();
    expect(map.animateCamera).toHaveBeenLastCalledWith(
      expect.objectContaining({
        center: { latitude: north(40).lat, longitude: north(40).lng },
        pitch: PITCH_3D_DEG,
      }),
      { duration: 400 },
    );

    await act(async () => store.getState().lookAt({ lat: 52.5215, lng: 13.4113 }));
    expect(map.animateCamera).toHaveBeenLastCalledWith(
      expect.objectContaining({
        center: { latitude: 52.5215, longitude: 13.4113 },
        pitch: PITCH_3D_DEG,
      }),
      { duration: 400 },
    );

    // Disable 3D: resets pitch and heading to 0 without forcing zoom.
    await fireEvent.press(toggle);
    expect(toggle.props.accessibilityState).toEqual({ selected: false });
    expect(map.animateCamera).toHaveBeenLastCalledWith({ pitch: 0, heading: 0 }, { duration: 400 });
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

  it('shows empty states when no stops or no departures are nearby (task 7.5)', async () => {
    answer = () =>
      new Response(JSON.stringify(nearby()), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    await renderMap(WEDDING);
    expect(await screen.findByText('No stops nearby')).toBeOnTheScreen();
    expect(
      screen.getByText(
        'No transit stops within 500 m. Move the map to look around Berlin and Brandenburg.',
      ),
    ).toBeOnTheScreen();

    // Stops exist, but none has upcoming departures in the 90-minute window.
    const quiet = station('de:11000:900009104', 'S+U Wedding');
    quiet.poles[0]!.directions[0]!.departures = [];
    answer = () =>
      new Response(JSON.stringify(nearby(quiet)), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    await renderMap(north(300));
    expect(await screen.findByText('No departures nearby')).toBeOnTheScreen();
    expect(
      screen.getByText('Stops in this area have no departures in the next 90 minutes.'),
    ).toBeOnTheScreen();
  });

  it('switches combined stops on and off at street zoom (task 7.8)', async () => {
    const dep = {
      tripId: '1',
      serviceDate: '2026-11-05',
      scheduled: new Date(Date.now() + 5 * 60_000).toISOString(),
      predicted: null,
      delaySeconds: null,
      source: 'scheduled' as const,
      cancelled: false,
    };
    const platform = (n: string, lat: number, headsign: string) => ({
      id: `wedding::${n}`,
      location: { lat, lng: 13.365 },
      platform: n,
      description: null,
      directions: [
        {
          line: 'U6',
          mode: 'ubahn' as const,
          color: null,
          headsign,
          isTerminal: false,
          isRing: false,
          departures: [{ ...dep, tripId: n }],
        },
      ],
    });
    const wedding = {
      id: 'de:11000:900009104',
      kind: 'station' as const,
      name: 'S+U Wedding',
      location: { lat: 52.543, lng: 13.365 },
      modes: ['ubahn' as const],
      // U6 Gleis 1 and 2, 10 m apart.
      poles: [platform('1', 52.543, 'Alt-Mariendorf'), platform('2', 52.54309, 'Alt-Tegel')],
    };
    answer = () =>
      new Response(JSON.stringify(nearby(wedding)), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });

    await renderMap(wedding.location);
    await screen.findByText('Alt-Tegel');
    await act(async () =>
      map.props?.onRegionChangeComplete?.(
        { latitude: 52.543, longitude: 13.365, latitudeDelta: 0.001, longitudeDelta: 0.001 },
        {},
      ),
    );
    // On by default: both platforms in one marker, as two sections.
    const toggle = screen.getByRole('switch', { name: 'Combine stops' });
    expect(toggle.props.accessibilityState).toEqual({ checked: true });
    expect(screen.getAllByTestId('marker')).toHaveLength(1);
    expect(screen.getByTestId('section-1')).toBeOnTheScreen();

    // Off: one marker per platform, as before 7.8.
    await fireEvent.press(toggle);
    expect(toggle.props.accessibilityState).toEqual({ checked: false });
    expect(screen.getAllByTestId('marker')).toHaveLength(2);
    expect(screen.queryByTestId('section-1')).toBeNull();

    await fireEvent.press(toggle);
    expect(screen.getAllByTestId('marker')).toHaveLength(1);
  });

  it('moves overlapping markers apart and back when zoomed in (task 7.9)', async () => {
    const first = station('de:11000:1', 'First stop');
    const second = {
      ...station('de:11000:2', 'Second stop'),
      location: { lat: 52.54285, lng: 13.366 }, // 17 m north of the first
    };
    second.poles[0]!.location = second.location;
    answer = () =>
      new Response(JSON.stringify(nearby(first, second)), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    await renderMap(WEDDING);
    await screen.findByText('Second stop → Jungfernheide');
    const streetZoom = {
      latitude: 52.5427,
      longitude: 13.366,
      latitudeDelta: 0.001,
      longitudeDelta: 0.001,
    };
    await act(async () => map.props?.onRegionChangeComplete?.(streetZoom, {}));

    // 17 m ≈ 15 points apart: the bubbles (170 × 90 until measured) overlap.
    // The first (nearest) stays on its stop; the second moves aside.
    const secondStop = { latitude: 52.54285, longitude: 13.366 };
    const markerAt = (i: number) => screen.getAllByTestId('marker')[i]!.props.coordinate;
    await waitFor(() => expect(markerAt(1)).not.toEqual(secondStop));
    expect(markerAt(0)).toEqual({ latitude: 52.5427, longitude: 13.366 });
    // Only the two markers: no line or dot back to the stop (user decision).
    expect(screen.getAllByTestId('marker')).toHaveLength(2);

    // Zoomed in far enough (100× the points per degree): both back on their stops.
    map.scale = 10_000_000;
    await act(async () =>
      map.props?.onRegionChangeComplete?.({ ...streetZoom, latitudeDelta: 0.0002 }, {}),
    );
    await waitFor(() => expect(markerAt(1)).toEqual(secondStop));
  });

  it('triggers haptic feedback on marker selection, 3D toggle, and retry (task 7.5)', async () => {
    const selectionSpy = jest.spyOn(haptics, 'hapticSelection');
    const lightSpy = jest.spyOn(haptics, 'hapticLight');

    await renderMap(WEDDING);
    const marker = await screen.findByTestId('marker');

    await fireEvent.press(marker);
    expect(selectionSpy).toHaveBeenCalledTimes(1);

    await fireEvent.press(screen.getByRole('button', { name: '3D' }));
    expect(lightSpy).toHaveBeenCalledTimes(1);

    selectionSpy.mockRestore();
    lightSpy.mockRestore();
  });
});

describe('transit layer', () => {
  it('selects the poles, and none when the layer is down', () => {
    const r = nearby(station('a', 'A'), station('b', 'B'));
    expect(transitLayer.select(r).map((s) => transitLayer.key(s))).toEqual(['a::1', 'b::1']);
    expect(transitLayer.select({ ...r, layers: {} })).toEqual([]);
  });
});
