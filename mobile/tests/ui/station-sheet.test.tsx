import { beforeEach, describe, expect, it, jest } from '@jest/globals';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { ApiError, createApiClient, type ApiClientOptions } from '@/api/client';
import { fetchStation, type StationResponse } from '@/api/station';
import type { Departure } from '@/core/departures';
import type { Direction } from '@/core/directions';
import type { Pole, Station } from '@/core/sheet';
import type { PoleFeature } from '@/layers/transit';
import { StationSheet } from '@/layers/transit/StationSheet';
import { createClockStore } from '@/state/clock';
import { createLocationStore } from '@/state/location';
import { MapScreen } from '@/ui/MapScreen';

import { sheet } from './bottom-sheet-mock';

jest.mock(
  'react-native-maps',
  () => jest.requireActual<typeof import('./maps-mock')>('./maps-mock').mapsMock,
);

const BASE = 'http://192.168.178.29:8080';
const t0 = Date.parse('2026-11-05T14:30:00+01:00');

function dep(time: string, cancelled = false): Departure {
  return {
    tripId: `t-${time}`,
    serviceDate: '2026-11-05',
    scheduled: `2026-11-05T${time}:00+01:00`,
    predicted: null,
    delaySeconds: null,
    source: 'scheduled',
    cancelled,
  };
}

function direction(
  line: string,
  headsign: string,
  departures: Departure[],
  mode: Direction['mode'] = 'bus',
): Direction {
  return { line, mode, color: null, headsign, isTerminal: false, isRing: false, departures };
}

function pole(
  id: string,
  platform: string | null,
  directions: Direction[],
  description: string | null = null,
): Pole {
  return { id, location: { lat: 52.5427, lng: 13.366 }, platform, description, directions };
}

const S41 = direction(
  'S41',
  'Ringbahn S 41',
  [
    dep('14:32'),
    {
      ...dep('14:37'),
      predicted: '2026-11-05T14:40:00+01:00',
      delaySeconds: 180,
      source: 'realtime',
    },
    dep('14:42'),
  ],
  'sbahn',
);
const M27 = direction('M27', 'S Jungfernheide', [dep('14:33', true), dep('14:43')]);
const RE2 = direction('RE2', 'Nauen', [], 'regional');

const full: Station = {
  id: 'de:11000:900009104',
  kind: 'station',
  name: 'S+U Wedding',
  location: { lat: 52.5427, lng: 13.366 },
  modes: ['regional', 'sbahn', 'bus'],
  poles: [
    pole('p1', '1', [S41]),
    pole(
      'p2',
      null,
      [M27, direction('N20', 'S Oranienburger Str.', [])],
      'Bushalt Müllerstraße ht. Lindower Straße',
    ),
    pole('p3', '5', [RE2]),
  ],
};

/** What /v1/nearby knew: only the pole inside the circle. */
const tapped: PoleFeature = { station: { ...full, poles: [full.poles[0]!] }, pole: full.poles[0]! };

function stationResponse(station: Station): StationResponse {
  return {
    meta: {
      generatedAt: '2026-11-05T14:30:05+01:00',
      attribution: ['Data: VBB (CC BY 4.0)'],
      layers: { transit: { version: 1, status: 'ok' } },
    },
    station,
  };
}

type FakeFetch = NonNullable<ApiClientOptions['fetch']>;
let requests: Request[];
let answer: () => Response | Promise<Response>;
const fakeFetch: FakeFetch = async (request) => {
  requests.push(request);
  return answer();
};
const json = (body: unknown, status = 200, headers: Record<string, string> = {}) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json', ...headers },
  });

beforeEach(() => {
  requests = [];
  answer = () => json(stationResponse(full), 200, { ETag: '"s1"' });
  sheet.props = null;
});

function wrapper({ children }: { children: ReactNode }) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { gcTime: Infinity } } });
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}

async function renderSheet() {
  const clock = createClockStore(() => t0);
  const client = createApiClient(BASE, { fetch: fakeFetch });
  await render(<StationSheet feature={tapped} client={client} clock={clock} />, { wrapper });
}

describe('StationSheet', () => {
  it('shows the whole station: running directions per pole, idle ones at the bottom', async () => {
    await renderSheet();
    expect(await screen.findByText('S Jungfernheide')).toBeOnTheScreen();
    // openapi-fetch encodes ":" as %3A; the backend decodes it (checked with curl).
    expect(decodeURIComponent(new URL(requests[0]!.url).pathname)).toBe(
      '/v1/stations/de:11000:900009104',
    );
    expect(screen.getByRole('header', { name: 'S+U Wedding' })).toBeOnTheScreen();
    expect(screen.queryByText('Loading all platforms…')).toBeNull();

    expect(screen.getByText('Platform 1')).toBeOnTheScreen();
    expect(screen.getByText('Müllerstraße ht. Lindower Straße')).toBeOnTheScreen(); // bus pole: VBB's description
    expect(screen.getByText('2 min')).toBeOnTheScreen(); // S41 14:32
    expect(screen.getByLabelText('10 min, live, 3 min late')).toHaveTextContent('10 min +3'); // S41 14:37 → 14:40
    expect(screen.getByText('12 min')).toBeOnTheScreen();
    const cancelled = screen.getByLabelText('3 min, cancelled'); // M27 14:33
    expect(cancelled).toHaveStyle({ textDecorationLine: 'line-through' });
    expect(screen.getByText('13 min')).toBeOnTheScreen();
    expect(screen.getByText(/^Green: live/)).toBeOnTheScreen();

    // Nothing coming: once each, after all running directions, platform noted.
    expect(screen.getByText('No departures soon')).toBeOnTheScreen();
    expect(screen.getByText('S Oranienburger Str.')).toBeOnTheScreen();
    expect(screen.getByText('Pl. 5')).toBeOnTheScreen();
    expect(screen.queryByText('Platform 5')).toBeNull(); // its pole has nothing running
    // VoiceOver labels for running and idle direction rows (task 7.5).
    expect(
      screen.getByLabelText(
        'S41 to Ringbahn S 41: 2 min, timetable; 10 min, live, 3 min late; 12 min, timetable',
      ),
    ).toBeOnTheScreen();
    expect(screen.getByLabelText('RE2 to Nauen, platform 5: no departures soon')).toBeOnTheScreen();
    const texts = screen.getAllByText(/./).map((t) => String(t.props.children));
    expect(texts.indexOf('No departures soon')).toBeGreaterThan(texts.indexOf('S Jungfernheide'));
    expect(texts.indexOf('Nauen')).toBeGreaterThan(texts.indexOf('No departures soon'));
    // Data attribution at the very end of the sheet.
    expect(texts[texts.length - 1]).toBe('Data: VBB (CC BY 4.0)');
  });

  it('shows what /v1/nearby had without a backend', async () => {
    const clock = createClockStore(() => t0);
    await render(<StationSheet feature={tapped} client={null} clock={clock} />, { wrapper });
    expect(screen.getByText('Ringbahn S 41')).toBeOnTheScreen();
    expect(screen.getByText('2 min')).toBeOnTheScreen();
    expect(screen.queryByText('Loading all platforms…')).toBeNull();
    expect(requests).toHaveLength(0);
  });

  it('keeps what it had when the station cannot be loaded', async () => {
    answer = () =>
      json({ error: { code: 'unavailable', message: 'no departures yet, try again later' } }, 503);
    await renderSheet();
    expect(
      await screen.findByText(
        'Could not load the whole station: no departures yet, try again later',
      ),
    ).toBeOnTheScreen();
    expect(screen.getByText('Ringbahn S 41')).toBeOnTheScreen();
  });

  it('says when nothing leaves soon', async () => {
    answer = () => json(stationResponse({ ...full, poles: [pole('p3', '5', [RE2])] }));
    await renderSheet();
    expect(await screen.findByText('No departures in the next 90 minutes.')).toBeOnTheScreen();
    expect(screen.getByText('Nauen')).toBeOnTheScreen();
  });

  it('formats Ringbahn ⟳/⟲ headsigns and shows only return directions at terminal stops (task 7.3)', async () => {
    const weddingAndTerminal: Station = {
      ...full,
      poles: [
        pole('p1', '1', [{ ...S41, isRing: true }]),
        pole('p2', '2', [
          {
            ...direction('S42', 'Ringbahn S 42', [dep('14:34')], 'sbahn'),
            isRing: true,
          },
          {
            ...direction('U6', 'S+U Wedding', [dep('14:31')], 'ubahn'),
            isTerminal: true,
          },
          direction('U6', 'U Alt-Mariendorf', [dep('14:36')], 'ubahn'),
        ]),
        pole('p3', '3', [
          {
            ...direction('S41', 'S Südkreuz Bhf', [], 'sbahn'),
            isRing: true,
          },
        ]),
      ],
    };
    answer = () => json(stationResponse(weddingAndTerminal));
    await renderSheet();
    expect(await screen.findByText('⟳ Ringbahn S 41')).toBeOnTheScreen();
    expect(screen.getByText('⟲ Ringbahn S 42')).toBeOnTheScreen();
    expect(screen.getByText('U Alt-Mariendorf')).toBeOnTheScreen();
    expect(screen.getByText('⟳ S Südkreuz Bhf')).toBeOnTheScreen();
    // The terminating U6 arrival ("S+U Wedding") is not shown as a departure row.
    expect(screen.getAllByText('S+U Wedding')).toHaveLength(1); // station header only
  });
});

describe('fetchStation', () => {
  it('revalidates with the kept ETag and keeps the data on 304', async () => {
    answer = () => new Response(null, { status: 304, headers: { ETag: '"s1"' } });
    const client = createApiClient(BASE, { fetch: fakeFetch });
    const previous = {
      response: stationResponse(full),
      etag: '"s1"',
      checkedAt: 1,
      clockOffsetMs: 0,
    };
    const got = await fetchStation(client, full.id, { previous, now: () => 31_000 });
    expect(requests[0]!.headers.get('If-None-Match')).toBe('"s1"');
    expect(got.response).toBe(previous.response);
    expect(got.checkedAt).toBe(31_000);
  });

  it('reports a 404 with the server message', async () => {
    answer = () =>
      json({ error: { code: 'not_found', message: 'no station "x" with departures' } }, 404);
    const client = createApiClient(BASE, { fetch: fakeFetch });
    await expect(fetchStation(client, 'x')).rejects.toEqual(
      new ApiError('no station "x" with departures', 404),
    );
  });
});

describe('MapScreen with the sheet', () => {
  it('opens the station sheet on a marker tap and closes it on swipe down', async () => {
    const soon = new Date(Date.now() + 5 * 60_000).toISOString();
    const live: Station = {
      ...full,
      poles: [
        pole('p1', '1', [
          direction('S41', 'Ringbahn S 41', [{ ...dep('14:32'), scheduled: soon }], 'sbahn'),
        ]),
      ],
    };
    answer = () => {
      // /v1/nearby first, then the station.
      const last = requests[requests.length - 1]!;
      return new URL(last.url).pathname === '/v1/nearby'
        ? json({
            meta: { generatedAt: '2026-11-05T14:30:05+01:00', attribution: [], layers: {} },
            layers: { transit: { layer: 'transit', features: [live] } },
          })
        : json(stationResponse(live));
    };
    await render(
      <MapScreen
        position={{ lat: 52.5427, lng: 13.366 }}
        source="live"
        baseUrl={{ ok: true, url: BASE }}
        client={createApiClient(BASE, { fetch: fakeFetch })}
        store={createLocationStore()}
        devTools={false}
      />,
      { wrapper },
    );
    const marker = await screen.findByTestId('marker');
    expect(screen.queryByTestId('bottom-sheet')).toBeNull();

    await fireEvent.press(marker);
    expect(screen.getByTestId('bottom-sheet')).toBeOnTheScreen();
    expect(sheet.props?.enablePanDownToClose).toBe(true);
    expect(screen.getByRole('header', { name: 'S+U Wedding' })).toBeOnTheScreen();
    await waitFor(() =>
      expect(requests.some((r) => new URL(r.url).pathname.startsWith('/v1/stations/'))).toBe(true),
    );

    await act(async () => sheet.props?.onClose?.());
    expect(screen.queryByTestId('bottom-sheet')).toBeNull();
  });
});
