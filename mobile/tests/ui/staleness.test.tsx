import { afterEach, beforeEach, describe, expect, it, jest } from '@jest/globals';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen } from '@testing-library/react-native';
import { Profiler, type ReactNode } from 'react';

import { ApiError, createApiClient, type ApiClientOptions, failureOf } from '@/api/client';
import type { NearbyResponse } from '@/api/nearby';
import type { Departure } from '@/core/departures';
import type { Direction } from '@/core/directions';
import { MIN_INTERVAL_MS } from '@/core/fetchPolicy';
import { STALE_AFTER_MS } from '@/core/staleness';
import { MINUTE, SECOND } from '@/core/time';
import type { PoleFeature } from '@/layers/transit';
import { LATE_RED, STALE_GREY } from '@/layers/transit/DepartureTime';
import { PoleMarker, TRACK_CHANGES_MS } from '@/layers/transit/PoleMarker';
import { StationSheet } from '@/layers/transit/StationSheet';
import { type ClockStore, createClockStore } from '@/state/clock';
import { createLocationStore } from '@/state/location';
import { MapScreen } from '@/ui/MapScreen';
import { StatusBanner } from '@/ui/StatusBanner';

jest.mock(
  'react-native-maps',
  () => jest.requireActual<typeof import('./maps-mock')>('./maps-mock').mapsMock,
);

const BASE = 'http://192.168.178.29:8080';
const WEDDING = { lat: 52.5427, lng: 13.366 };

/** Departures relative to `from`, so they are still ahead after minutes of fake time. */
function departures(from: number): Departure[] {
  const iso = (min: number) => new Date(from + min * MINUTE).toISOString();
  const scheduled = (min: number): Departure => ({
    tripId: `t-${min}`,
    serviceDate: '2026-11-05',
    scheduled: iso(min),
    predicted: null,
    delaySeconds: null,
    source: 'scheduled',
    cancelled: false,
  });
  return [
    // 20 min ahead, 2 min late.
    { ...scheduled(18), predicted: iso(20), delaySeconds: 120, source: 'realtime' },
    scheduled(40),
  ];
}

function directions(from: number): Direction[] {
  const [late, timetable] = departures(from);
  const direction = (line: string, headsign: string, d: Departure): Direction => ({
    line,
    mode: 'bus',
    color: null,
    headsign,
    isTerminal: false,
    isRing: false,
    departures: [d],
  });
  return [
    direction('M27', 'S Jungfernheide', late!),
    direction('247', 'S Nordbahnhof', timetable!),
  ];
}

function feature(from: number): PoleFeature {
  const pole = {
    id: 'de:11000:900009104::1',
    location: WEDDING,
    platform: null,
    description: null,
    directions: directions(from),
  };
  return {
    station: {
      id: 'de:11000:900009104',
      kind: 'station',
      name: 'S+U Wedding',
      location: WEDDING,
      modes: ['bus'],
      poles: [pole],
    },
    pole,
  };
}

function nearby(from: number): NearbyResponse {
  return {
    meta: {
      generatedAt: new Date(from).toISOString(),
      attribution: ['Data: VBB (CC BY 4.0)'],
      layers: { transit: { version: 1, status: 'ok' } },
    },
    layers: { transit: { layer: 'transit', features: [feature(from).station] } },
  };
}

afterEach(() => {
  jest.useRealTimers();
});

describe('StatusBanner', () => {
  const t0 = Date.parse('2026-11-05T14:30:00+01:00');
  let deviceNow: number;
  let clock: ClockStore;
  const at = (ms: number) =>
    act(async () => {
      deviceNow = ms;
      clock.getState().tick();
    });

  beforeEach(() => {
    deviceNow = t0;
    clock = createClockStore(() => deviceNow);
    // A server clock 4 days off must not change the data's age.
    clock.getState().setOffset(-4 * 24 * 60 * MINUTE);
  });

  it('is silent while fresh, shows the age from 60 s, warns after 5 min', async () => {
    const onRender = jest.fn();
    await render(
      <Profiler id="banner" onRender={onRender}>
        <StatusBanner
          checkedAt={t0}
          failure={null}
          onRetry={() => {}}
          retrying={false}
          clock={clock}
        />
      </Profiler>,
    );
    expect(screen.queryByRole('alert')).toBeNull();

    await at(t0 + 2 * MINUTE);
    expect(screen.getByText('Updated 2 min ago')).toBeOnTheScreen();
    expect(screen.queryByText('Live times may be out of date')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Try again' })).toBeNull();

    // Seconds pass, the text does not change: no re-render.
    onRender.mockClear();
    await at(t0 + 2 * MINUTE + 30 * SECOND);
    expect(onRender).not.toHaveBeenCalled();

    await at(t0 + STALE_AFTER_MS + MINUTE);
    expect(screen.getByText('Updated 6 min ago')).toBeOnTheScreen();
    expect(screen.getByText('Live times may be out of date')).toBeOnTheScreen();
  });

  it('says that requests fail, whatever the age, and offers to try again', async () => {
    const onRetry = jest.fn();
    await render(
      <StatusBanner
        checkedAt={t0}
        failure="unreachable"
        onRetry={onRetry}
        retrying={false}
        clock={clock}
      />,
    );
    expect(screen.getByText('No connection — showing last known data')).toBeOnTheScreen();
    expect(screen.getByText('updated just now')).toBeOnTheScreen();
    await fireEvent.press(screen.getByRole('button', { name: 'Try again' }));
    expect(onRetry).toHaveBeenCalledTimes(1);
  });

  it('tells a server error from an unreachable backend', () => {
    expect(failureOf(new ApiError('HTTP 503', 503))).toBe('server');
    expect(failureOf(new ApiError('no answer within 5 s'))).toBe('unreachable');
    expect(failureOf(new TypeError('Network request failed'))).toBe('unreachable');
  });
});

describe('stale data in the transit layer', () => {
  const t0 = Date.parse('2026-11-05T14:30:00+01:00');
  const clock = createClockStore(() => t0);
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { gcTime: Infinity } } })}
    >
      {children}
    </QueryClientProvider>
  );

  it('greys out live countdowns on the marker and redraws it', async () => {
    jest.useFakeTimers();
    const f = feature(t0);
    const view = await render(<PoleMarker feature={f} clock={clock} />);
    expect(screen.getByLabelText('20 min, live, 2 min late')).toHaveStyle({ color: LATE_RED });
    await act(async () => jest.advanceTimersByTime(TRACK_CHANGES_MS));
    const tracks = () => screen.getByTestId('marker').props.tracksViewChanges as boolean;
    expect(tracks()).toBe(false);

    await view.rerender(<PoleMarker feature={f} clock={clock} stale />);
    const late = screen.getByLabelText('20 min, outdated live time, 2 min late');
    expect(late).toHaveTextContent('20 min +2');
    expect(late).toHaveStyle({ color: STALE_GREY });
    // Timetable times keep their colour: they are what still holds.
    expect(screen.getByLabelText('40 min, timetable')).toHaveStyle({ color: '#111111' });
    expect(tracks()).toBe(true); // the native marker snapshot is redrawn
    expect(
      screen.getByLabelText(/M27 to S Jungfernheide: 20 min, outdated live time, 2 min late/),
    ).toBeOnTheScreen();
  });

  it('greys out live times in the station sheet and says why', async () => {
    const view = await render(<StationSheet feature={feature(t0)} client={null} clock={clock} />, {
      wrapper,
    });
    expect(screen.getByText(/^Green: live, on time/)).toBeOnTheScreen();

    await view.rerender(<StationSheet feature={feature(t0)} client={null} clock={clock} stale />);
    expect(screen.getByLabelText('20 min, outdated live time, 2 min late')).toHaveStyle({
      color: STALE_GREY,
    });
    expect(screen.getByText(/^Grey: live times from more than 5 minutes ago/)).toBeOnTheScreen();
    expect(screen.queryByText(/^Green: live, on time/)).toBeNull();
  });
});

// Minutes left depend on the second the test runs in.
const LIVE = /^(19|20|1d) min, live, 2 min late$/;

describe('MapScreen without a connection (task 6.2)', () => {
  type FakeFetch = NonNullable<ApiClientOptions['fetch']>;
  let online: boolean;
  let requests: number;
  const fakeFetch: FakeFetch = async () => {
    requests++;
    if (!online) {
      throw new TypeError('Network request failed');
    }
    return new Response(JSON.stringify(nearby(Date.now())), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    });
  };

  async function renderMap() {
    const queryClient = new QueryClient({ defaultOptions: { queries: { gcTime: Infinity } } });
    await render(
      <QueryClientProvider client={queryClient}>
        <MapScreen
          position={WEDDING}
          source="live"
          baseUrl={{ ok: true, url: BASE }}
          client={createApiClient(BASE, { fetch: fakeFetch })}
          store={createLocationStore()}
          devTools={false}
        />
      </QueryClientProvider>,
    );
  }

  beforeEach(() => {
    online = true;
    requests = 0;
  });

  it('keeps the departures, says so, greys out live times after 5 min, recovers', async () => {
    jest.useFakeTimers(); // also moves Date.now
    await renderMap();
    expect(await screen.findByText('S Jungfernheide')).toBeOnTheScreen();
    expect(screen.queryByRole('alert')).toBeNull();
    expect(screen.getByLabelText(LIVE)).toHaveStyle({ color: LATE_RED });

    // The connection goes: the next request (data 30 s old) fails.
    online = false;
    await act(() => jest.advanceTimersByTimeAsync(31 * SECOND));
    expect(await screen.findByText('No connection — showing last known data')).toBeOnTheScreen();
    expect(screen.getByText('updated just now')).toBeOnTheScreen();
    // The data stays; no dialog over the map.
    expect(screen.getByText('S Jungfernheide')).toBeOnTheScreen();
    expect(screen.queryByText('Could not load departures')).toBeNull();
    expect(screen.getByLabelText(/^1\d min, live, 2 min late$/)).toHaveStyle({ color: LATE_RED });

    // Two minutes offline: the age counts from the last answer.
    await act(() => jest.advanceTimersByTimeAsync(2 * MINUTE));
    expect(screen.getByText(/^updated [23] min ago$/)).toBeOnTheScreen();

    // More than five: live values are no longer shown as live.
    await act(() => jest.advanceTimersByTimeAsync(4 * MINUTE));
    expect(
      screen.getByText(/^updated \d min ago, live times may be out of date$/),
    ).toBeOnTheScreen();
    expect(screen.getByLabelText(/^1\d min, outdated live time, 2 min late$/)).toHaveStyle({
      color: STALE_GREY,
    });
    expect(screen.getByLabelText(/^3\d min, timetable$/)).toHaveStyle({ color: '#111111' });

    // Back online: "Try again" requests (after the 10 s floor) and all is normal.
    online = true;
    const before = requests;
    await fireEvent.press(screen.getByRole('button', { name: 'Try again' }));
    await act(() => jest.advanceTimersByTimeAsync(MIN_INTERVAL_MS));
    expect(requests).toBeGreaterThan(before);
    expect(screen.queryByRole('alert')).toBeNull();
    expect(screen.getByLabelText(LIVE)).toHaveStyle({ color: LATE_RED });
  });

  it('without any data shows the error with "Try again", as before', async () => {
    jest.useFakeTimers();
    online = false;
    await renderMap();
    expect(await screen.findByText('Could not load departures')).toBeOnTheScreen();
    expect(screen.getByText('Network request failed')).toBeOnTheScreen();
    expect(screen.queryByRole('alert')).toBeNull();
  });
});
