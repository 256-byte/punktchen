import { afterEach, describe, expect, it, jest } from '@jest/globals';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen } from '@testing-library/react-native';

import { createApiClient, type ApiClientOptions } from '@/api/client';
import type { NearbyResponse } from '@/api/nearby';
import type { Departure } from '@/core/departures';
import type { Direction } from '@/core/directions';
import { MINUTE, SECOND } from '@/core/time';
import { STALE_GREY } from '@/layers/transit/DepartureTime';
import { createLocationStore } from '@/state/location';
import { MapScreen } from '@/ui/MapScreen';

jest.mock(
  'react-native-maps',
  () => jest.requireActual<typeof import('./maps-mock')>('./maps-mock').mapsMock,
);

const BASE = 'http://192.168.178.29:8080';
const WEDDING = { lat: 52.5427, lng: 13.366 };

/** /v1/nearby as fetched at `from`: up to three departures per direction. */
function nearby(from: number): NearbyResponse {
  const iso = (min: number) => new Date(from + min * MINUTE).toISOString();
  const dep = (min: number, predictedMin: number | null = null): Departure => ({
    tripId: `t-${min}`,
    serviceDate: '2026-11-05',
    scheduled: iso(min),
    predicted: predictedMin === null ? null : iso(predictedMin),
    delaySeconds: predictedMin === null ? null : (predictedMin - min) * 60,
    source: predictedMin === null ? 'scheduled' : 'realtime',
    cancelled: false,
  });
  const direction = (line: string, headsign: string, departures: Departure[]): Direction => ({
    line,
    mode: 'bus',
    color: null,
    headsign,
    isTerminal: false,
    isRing: false,
    departures,
  });
  return {
    meta: {
      generatedAt: new Date(from).toISOString(),
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
                  direction('U6', 'Alt-Mariendorf', [dep(3), dep(8), dep(14)]),
                  direction('M27', 'S Jungfernheide', [dep(4), dep(10, 12)]),
                  direction('247', 'S Nordbahnhof', [dep(2), dep(5)]),
                ],
              },
            ],
          },
        ],
      },
    },
  };
}

afterEach(() => {
  jest.useRealTimers();
});

describe('ten minutes without a connection (task 6.3)', () => {
  it('drops the departures that have left and keeps counting down', async () => {
    jest.useFakeTimers(); // also moves Date.now
    let online = true;
    const fakeFetch: NonNullable<ApiClientOptions['fetch']> = async () => {
      if (!online) {
        throw new TypeError('Network request failed');
      }
      return new Response(JSON.stringify(nearby(Date.now())), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    };
    await render(
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { gcTime: Infinity } } })}
      >
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

    // As fetched: every direction with its next departure.
    expect(await screen.findByText('Alt-Mariendorf')).toBeOnTheScreen();
    expect(screen.getAllByLabelText(/^\d min, timetable$/)).toHaveLength(3); // +3, +4, +2
    expect(screen.getByText('S Jungfernheide')).toBeOnTheScreen(); // M27 +4
    expect(screen.getByText('S Nordbahnhof')).toBeOnTheScreen(); // 247 +2

    // The connection goes; ten and a half minutes pass.
    online = false;
    await act(() => jest.advanceTimersByTimeAsync(10 * MINUTE + 30 * SECOND));
    expect(screen.getByText('No connection — showing last known data')).toBeOnTheScreen();
    expect(screen.getByText('updated 10 min ago, live times may be out of date')).toBeOnTheScreen();

    // U6: +3 and +8 have left, +14 is next.
    expect(screen.getByLabelText(/^[34] min, timetable$/)).toBeOnTheScreen();
    // M27: +4 has left; the next one still counts down to its last predicted
    // time (+12), shown as outdated.
    const m27 = screen.getByLabelText(/^[12] min, outdated live time, 2 min late$/);
    expect(m27).toHaveStyle({ color: STALE_GREY });
    // 247: every known departure has left. Its row is gone; nothing is made up.
    expect(screen.queryByText('S Nordbahnhof')).toBeNull();
    expect(screen.queryByText('now')).toBeNull();

    // The station sheet (the station cannot be loaded: what /v1/nearby had).
    await fireEvent.press(screen.getByTestId('marker'));
    await act(() => jest.advanceTimersByTimeAsync(SECOND));
    const texts = screen.getAllByText(/./).map((n) => String(n.props.children));
    expect(texts).toContain('No newer data');
    expect(texts).not.toContain('No departures soon');
    expect(texts.indexOf('S Nordbahnhof')).toBeGreaterThan(texts.indexOf('No newer data'));
    // U6 lists only what is still to come: one time, not three.
    expect(screen.getAllByLabelText(/^\d+ min, timetable$/)).toHaveLength(2); // marker + sheet

    // Five more minutes: everything known has left.
    await act(() => jest.advanceTimersByTimeAsync(5 * MINUTE));
    expect(screen.queryByTestId('marker')).toBeNull();
    expect(
      screen.getByText(
        'The saved departures have left. Newer ones load when the connection is back.',
      ),
    ).toBeOnTheScreen();

    // Back online: the next request brings the map back.
    online = true;
    await act(() => jest.advanceTimersByTimeAsync(MINUTE + 10 * SECOND));
    expect(screen.getByTestId('marker')).toBeOnTheScreen();
    expect(screen.queryByRole('alert')).toBeNull();
  });
});
