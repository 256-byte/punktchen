import { afterEach, beforeEach, describe, expect, it, jest } from '@jest/globals';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { createApiClient, type ApiClientOptions } from '@/api/client';
import type { NearbyResponse } from '@/api/nearby';
import type { Fountain, Toilet } from '@/core/amenities';
import { FountainSheet, ToiletSheet } from '@/layers/amenities/AmenitySheet';
import { createClockStore } from '@/state/clock';
import { createLocationStore } from '@/state/location';
import { MapScreen } from '@/ui/MapScreen';

jest.mock(
  'react-native-maps',
  () => jest.requireActual<typeof import('./maps-mock')>('./maps-mock').mapsMock,
);

const BASE = 'http://192.168.178.29:8080';
const ALEX = { lat: 52.5215, lng: 13.4113 };

const toilet = (over: Partial<Toilet> = {}): Toilet => ({
  id: 'toiletten.2',
  kind: 'toilet',
  location: { lat: 52.52146, lng: 13.41295 },
  distanceMeters: 158,
  address: 'WC-Center Alexanderplatz 1',
  district: 'Mitte',
  operator: 'Wall GmbH',
  eventOnly: false,
  openingHours: 'Mo-Sa 07:00-22:00\nSo 10:00-22:00',
  open24h: false,
  feeEuro: 0.5,
  payment: 'Karte/App',
  accessible: false,
  mostlyAccessible: true,
  changingTable: true,
  ...over,
});

const fountain = (over: Partial<Fountain> = {}): Fountain => ({
  id: 'trinkwasserbrunnen.7',
  location: { lat: 52.5168, lng: 13.4087 },
  distanceMeters: 555,
  place: 'Poststr. 11 (Nikolaiviertel)',
  district: 'Mitte',
  model: 'Kaiser',
  builtYear: 2019,
  season: { fromMonth: 5, toMonth: 10 },
  outOfService: null,
  ...over,
});

function nearby(toilets: Toilet[], fountains: Fountain[]): NearbyResponse {
  return {
    meta: {
      generatedAt: new Date().toISOString(),
      attribution: ['Data: Geoportal Berlin (dl-de/zero-2-0)'],
      layers: {
        transit: { version: 1, status: 'ok' },
        toilets: { version: 7, status: 'ok' },
        fountains: { version: 9, status: 'ok' },
      },
    },
    layers: {
      transit: { layer: 'transit', features: [] },
      toilets: { layer: 'toilets', features: toilets },
      fountains: { layer: 'fountains', features: fountains },
    },
  };
}

type FakeFetch = NonNullable<ApiClientOptions['fetch']>;
let requests: URL[];
let body: NearbyResponse;
const fakeFetch: FakeFetch = async (request) => {
  requests.push(new URL(request.url));
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
};

async function renderMap() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { gcTime: Infinity } } });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
  await render(
    <MapScreen
      position={ALEX}
      source="live"
      baseUrl={{ ok: true, url: BASE }}
      client={createApiClient(BASE, { fetch: fakeFetch })}
      store={createLocationStore()}
      devTools={false}
    />,
    { wrapper },
  );
}

beforeEach(() => {
  requests = [];
  body = nearby([toilet()], [fountain()]);
});

afterEach(() => {
  jest.useRealTimers();
});

describe('amenity layers on the map (Phase 9)', () => {
  it('asks for all layers in one request and draws a marker per toilet and fountain', async () => {
    jest.useFakeTimers({ now: new Date('2026-07-15T12:00:00+02:00'), advanceTimers: true });
    // Built on the fake date: generatedAt sets the app's server clock (ADR-0007).
    body = nearby([toilet()], [fountain()]);
    await renderMap();
    await waitFor(() => expect(screen.getAllByTestId('amenity-marker')).toHaveLength(2));
    expect(requests[0]?.searchParams.get('layers')).toBe('transit,toilets,fountains');
    expect(screen.getByTestId('amenity-toilet')).toBeOnTheScreen();
    // July: in season, not greyed (after the app clock's first tick on the fake date).
    await waitFor(() => expect(screen.getByTestId('amenity-fountain')).toBeOnTheScreen(), {
      timeout: 3000,
    });
    expect(
      screen.getByLabelText(/^Public toilet, WC-Center Alexanderplatz 1\. €0\.50/),
    ).toBeTruthy();
  });

  it('greys fountains out of season and toilets open only at events (user decision 2026-11-18)', async () => {
    jest.useFakeTimers({ now: new Date('2026-11-18T12:00:00+01:00'), advanceTimers: true });
    body = nearby(
      [toilet({ id: 't.olympia', eventOnly: true, openingHours: 'bei Veranstaltungen' })],
      [fountain()],
    );
    await renderMap();
    expect(await screen.findByTestId('amenity-fountain-dimmed')).toBeOnTheScreen();
    expect(screen.getByTestId('amenity-toilet-dimmed')).toBeOnTheScreen();
    expect(
      screen.getByLabelText(
        'Drinking fountain, Poststr. 11 (Nikolaiviertel). Off season · runs May–October',
      ),
    ).toBeTruthy();
  });

  it('switches a layer off and on without a new request', async () => {
    await renderMap();
    await waitFor(() => expect(screen.getAllByTestId('amenity-marker')).toHaveLength(2));
    const toilets = screen.getByRole('switch', { name: 'Public toilets' });
    expect(toilets.props.accessibilityState).toEqual({ checked: true });

    await fireEvent.press(toilets);
    expect(screen.getAllByTestId('amenity-marker')).toHaveLength(1);
    expect(screen.queryByTestId('amenity-toilet')).toBeNull();
    expect(toilets.props.accessibilityState).toEqual({ checked: false });

    await fireEvent.press(screen.getByRole('switch', { name: 'Drinking fountains' }));
    expect(screen.queryAllByTestId('amenity-marker')).toHaveLength(0);

    await fireEvent.press(toilets);
    expect(screen.getAllByTestId('amenity-marker')).toHaveLength(1);
    expect(requests).toHaveLength(1);
  });

  it('opens the details of a tapped toilet', async () => {
    await renderMap();
    await waitFor(() => expect(screen.getAllByTestId('amenity-marker')).toHaveLength(2));
    await act(async () => fireEvent.press(screen.getAllByTestId('amenity-marker')[0]!));
    const sheet = screen.getByTestId('bottom-sheet');
    expect(within(sheet).getByText('WC-Center Alexanderplatz 1')).toBeOnTheScreen();
    expect(within(sheet).getByText('Card or app only')).toBeOnTheScreen();
  });
});

describe('ToiletSheet', () => {
  it('lists hours, fee, payment, access, baby care and operator', async () => {
    await render(<ToiletSheet feature={toilet()} />);
    for (const text of [
      'WC-Center Alexanderplatz 1',
      'Mitte · about 160 m',
      'Mo-Sa 07:00-22:00\nSo 10:00-22:00',
      '€0.50',
      'Card or app only',
      'Mostly barrier-free',
      'Changing table',
      'Wall GmbH',
      'Data: Geoportal Berlin (dl-de/zero-2-0)',
    ]) {
      expect(screen.getByText(text)).toBeOnTheScreen();
    }
    expect(screen.queryByTestId('amenity-note')).toBeNull();
  });

  it('notes a toilet open only during funerals, and leaves out what is unknown', async () => {
    await render(
      <ToiletSheet
        feature={toilet({
          kind: 'urinal',
          address: 'Friedhof Columbiadamm',
          eventOnly: true,
          openingHours: 'Mit Schlüssel während Trauerfeiern',
          payment: null,
          operator: null,
          district: null,
          feeEuro: 0,
        })}
      />,
    );
    expect(screen.getByText('Urinal · Friedhof Columbiadamm')).toBeOnTheScreen();
    expect(screen.getByText('Only open during funerals (with a key)')).toBeOnTheScreen();
    expect(screen.getByText('Free')).toBeOnTheScreen();
    expect(screen.queryByText('Payment')).toBeNull();
    expect(screen.queryByText('Operator')).toBeNull();
  });
});

describe('FountainSheet', () => {
  const clockAt = (iso: string) => createClockStore(() => new Date(iso).getTime());

  it('says why a fountain does not run: season or service', async () => {
    const { rerender } = await render(
      <FountainSheet feature={fountain()} clock={clockAt('2026-11-18T12:00:00+01:00')} />,
    );
    expect(screen.getByTestId('amenity-note')).toHaveTextContent('Off season · runs May–October');
    expect(screen.getByText('Kaiser')).toBeOnTheScreen();
    expect(screen.getByText('2019')).toBeOnTheScreen();

    await rerender(
      <FountainSheet
        feature={fountain({ outOfService: 'zur Zeit wegen Reparatur außer Betrieb' })}
        clock={clockAt('2026-07-01T12:00:00+02:00')}
      />,
    );
    expect(screen.getByTestId('amenity-note')).toHaveTextContent(
      'Out of service: zur Zeit wegen Reparatur außer Betrieb',
    );
  });

  it('has no note while it runs', async () => {
    await render(
      <FountainSheet feature={fountain()} clock={clockAt('2026-07-01T12:00:00+02:00')} />,
    );
    expect(screen.queryByTestId('amenity-note')).toBeNull();
    expect(screen.getAllByText('Running · season May–October').length).toBeGreaterThan(0);
  });
});
