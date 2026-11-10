// Design tokens and theme (task 7.1): every text colour is readable on the
// surfaces of its mode, both modes have the same tokens, and components
// follow the system's light or dark mode.
import { afterEach, beforeEach, describe, expect, it, jest } from '@jest/globals';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, renderHook, screen } from '@testing-library/react-native';
import * as ReactNative from 'react-native';
import { type ColorSchemeName, Text } from 'react-native';

import { createApiClient } from '@/api/client';
import type { NearbyResponse } from '@/api/nearby';
import { contrastRatio } from '@/core/color';
import type { PoleFeature } from '@/layers/transit';
import { PoleMarker, TRACK_CHANGES_MS } from '@/layers/transit/PoleMarker';
import { StationSheet } from '@/layers/transit/StationSheet';
import { createClockStore } from '@/state/clock';
import { createLocationStore } from '@/state/location';
import { LocationGate } from '@/ui/LocationGate';
import { MapScreen } from '@/ui/MapScreen';
import { StatusBanner } from '@/ui/StatusBanner';
import { schemeOf, themedStyles, useTheme } from '@/ui/theme';
import { type Colors, type ColorScheme, themes } from '@/ui/tokens';

import { sheet } from './bottom-sheet-mock';
import { map } from './maps-mock';

jest.mock(
  'react-native-maps',
  () => jest.requireActual<typeof import('./maps-mock')>('./maps-mock').mapsMock,
);

let currentScheme: ColorSchemeName = 'light';

beforeEach(() => {
  currentScheme = 'light';
  jest.spyOn(ReactNative, 'useColorScheme').mockImplementation(() => currentScheme);
});

afterEach(() => {
  jest.restoreAllMocks();
  jest.useRealTimers();
});

const SCHEMES: ColorScheme[] = ['light', 'dark'];

/** Text colours and the minimum contrast they need (WCAG AA). */
const TEXT_ON_SURFACES: [keyof Colors, number][] = [
  ['text', 4.5],
  ['textSecondary', 4.5],
  ['textTertiary', 4.5],
  ['accentText', 4.5],
  ['danger', 4.5],
  ['success', 4.5],
  ['live', 4.5],
  ['late', 4.5],
  ['stale', 4.5],
];

describe('tokens', () => {
  it('both modes define the same colours', () => {
    expect(Object.keys(themes.dark.colors).sort()).toEqual(Object.keys(themes.light.colors).sort());
  });

  describe.each(SCHEMES)('%s', (scheme) => {
    const c = themes[scheme].colors;

    it.each(TEXT_ON_SURFACES)('%s is readable on background and surface', (token, min) => {
      expect(contrastRatio(c[token], c.background)).toBeGreaterThanOrEqual(min);
      expect(contrastRatio(c[token], c.surface)).toBeGreaterThanOrEqual(min);
    });

    it('button and banner pairs are readable', () => {
      expect(contrastRatio(c.onAccent, c.accent)).toBeGreaterThanOrEqual(4.5);
      expect(contrastRatio(c.warningText, c.warningSurface)).toBeGreaterThanOrEqual(4.5);
      expect(contrastRatio(c.onInverse, c.inverseSurface)).toBeGreaterThanOrEqual(4.5);
    });

    it('greyed-out live times differ from timetable times', () => {
      // Stale live times must not look like the timetable ones (task 6.2).
      expect(contrastRatio(c.stale, c.text)).toBeGreaterThanOrEqual(2);
    });

    it('the warning border stands out from its surface (UI part, 3:1)', () => {
      expect(contrastRatio(c.warningBorder, c.warningSurface)).toBeGreaterThanOrEqual(3);
    });
  });

  it('dark mode is dark and light mode is light', () => {
    expect(contrastRatio(themes.light.colors.surface, '#FFFFFF')).toBeLessThan(1.1);
    expect(contrastRatio(themes.dark.colors.surface, '#000000')).toBeLessThan(1.5);
  });
});

describe('schemeOf', () => {
  it('is dark only when the system says so', () => {
    expect(schemeOf('dark')).toBe('dark');
    expect(schemeOf('light')).toBe('light');
    expect(schemeOf('unspecified')).toBe('light');
    expect(schemeOf(null)).toBe('light');
    expect(schemeOf(undefined)).toBe('light');
  });
});

describe('useTheme and themedStyles', () => {
  const useStyles = themedStyles((t) => ({ label: { color: t.colors.text } }));
  function Label() {
    const styles = useStyles();
    return <Text style={styles.label}>Hello</Text>;
  }

  it('follow the system appearance', async () => {
    currentScheme = 'light';
    const { result, rerender } = await renderHook(() => ({
      theme: useTheme(),
      styles: useStyles(),
    }));
    expect(result.current.theme).toBe(themes.light);
    const lightStyles = result.current.styles;

    currentScheme = 'dark';
    await rerender({});
    expect(result.current.theme).toBe(themes.dark);
    expect(result.current.styles).not.toBe(lightStyles);

    currentScheme = 'light';
    await rerender({});
    expect(result.current.styles).toBe(lightStyles); // created once per mode
  });

  it('colour text by the mode', async () => {
    currentScheme = 'dark';
    await render(<Label />);
    expect(screen.getByText('Hello')).toHaveStyle({ color: themes.dark.colors.text });
  });
});

const t0 = Date.parse('2026-11-05T14:30:00+01:00');

const weddingFeature: PoleFeature = {
  station: {
    id: 'de:11000:900009104',
    kind: 'station',
    name: 'S+U Wedding',
    location: { lat: 52.5427, lng: 13.366 },
    modes: ['ubahn', 'bus'],
    poles: [
      {
        id: 'de:11000:900009104::1',
        location: { lat: 52.5427, lng: 13.366 },
        platform: '1',
        description: null,
        directions: [
          {
            line: 'U6',
            mode: 'ubahn',
            color: '#8C6DAB',
            headsign: 'U Alt-Mariendorf',
            isTerminal: false,
            isRing: false,
            departures: [
              {
                tripId: 't-1',
                serviceDate: '2026-11-05',
                scheduled: '2026-11-05T14:33:00+01:00',
                predicted: '2026-11-05T14:33:00+01:00',
                delaySeconds: 0,
                source: 'realtime',
                cancelled: false,
              },
              {
                tripId: 't-2',
                serviceDate: '2026-11-05',
                scheduled: '2026-11-05T14:38:00+01:00',
                predicted: '2026-11-05T14:40:00+01:00',
                delaySeconds: 120,
                source: 'realtime',
                cancelled: false,
              },
              {
                tripId: 't-3',
                serviceDate: '2026-11-05',
                scheduled: '2026-11-05T14:43:00+01:00',
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
  pole: null as unknown as PoleFeature['pole'],
};
weddingFeature.pole = weddingFeature.station.poles[0]!;

describe('screens in light and dark mode', () => {
  it('PoleMarker colours its bubble and departure times by the mode and redraws on a switch', async () => {
    jest.useFakeTimers();
    const clock = createClockStore(() => t0);
    const { rerender } = await render(<PoleMarker feature={weddingFeature} clock={clock} />);

    const bubble = () => screen.getByLabelText(/^S\+U Wedding/);
    expect(bubble()).toHaveStyle({ backgroundColor: themes.light.colors.surface });
    expect(screen.getByText('U Alt-Mariendorf')).toHaveStyle({ color: themes.light.colors.text });
    expect(screen.getByLabelText('3 min, live, on time')).toHaveStyle({
      color: themes.light.colors.live,
    });
    // Line badges keep their own colour in both modes.
    expect(screen.getByText('U6')).toHaveStyle({ color: '#FFFFFF' });

    await act(async () => jest.advanceTimersByTime(TRACK_CHANGES_MS));
    expect(screen.getByTestId('marker').props.tracksViewChanges).toBe(false);

    currentScheme = 'dark';
    await rerender(<PoleMarker feature={weddingFeature} clock={clock} />);
    expect(bubble()).toHaveStyle({
      backgroundColor: themes.dark.colors.surface,
      borderColor: themes.dark.colors.border,
    });
    expect(screen.getByText('U Alt-Mariendorf')).toHaveStyle({ color: themes.dark.colors.text });
    expect(screen.getByLabelText('3 min, live, on time')).toHaveStyle({
      color: themes.dark.colors.live,
    });
    expect(screen.getByText('U6')).toHaveStyle({ color: '#FFFFFF' });
    // Native marker snapshot refreshes on a mode switch.
    expect(screen.getByTestId('marker').props.tracksViewChanges).toBe(true);
  });

  it('StationSheet uses dark colours and names white as the timetable colour in dark mode', async () => {
    const clock = createClockStore(() => t0);
    const queryClient = new QueryClient();
    const { rerender } = await render(
      <QueryClientProvider client={queryClient}>
        <StationSheet feature={weddingFeature} client={null} clock={clock} />
      </QueryClientProvider>,
    );
    expect(screen.getByText('S+U Wedding')).toHaveStyle({
      color: themes.light.colors.text,
    });
    expect(screen.getByText(/Black: timetable only/)).toBeOnTheScreen();

    currentScheme = 'dark';
    await rerender(
      <QueryClientProvider client={queryClient}>
        <StationSheet feature={weddingFeature} client={null} clock={clock} />
      </QueryClientProvider>,
    );
    expect(screen.getByText('S+U Wedding')).toHaveStyle({
      color: themes.dark.colors.text,
    });
    expect(screen.getByLabelText('3 min, live, on time')).toHaveStyle({
      color: themes.dark.colors.live,
    });
    expect(screen.getByLabelText('10 min, live, 2 min late')).toHaveStyle({
      color: themes.dark.colors.late,
    });
    expect(screen.getByLabelText('13 min, timetable')).toHaveStyle({
      color: themes.dark.colors.textSecondary,
    });
    expect(screen.getByText(/White: timetable only/)).toBeOnTheScreen();

    await rerender(
      <QueryClientProvider client={queryClient}>
        <StationSheet feature={weddingFeature} client={null} stale clock={clock} />
      </QueryClientProvider>,
    );
    expect(screen.getByLabelText('3 min, outdated live time, on time')).toHaveStyle({
      color: themes.dark.colors.stale,
    });
    expect(screen.getByText(/Grey: live times.*White: timetable only/)).toBeOnTheScreen();
  });

  it('MapScreen sets the map and bottom sheet to the active mode', async () => {
    currentScheme = 'dark';
    const soon = new Date(Date.now() + 5 * 60_000).toISOString();
    const liveStation = {
      ...weddingFeature.station,
      poles: [
        {
          ...weddingFeature.pole,
          directions: [
            {
              ...weddingFeature.pole.directions[0]!,
              departures: [
                {
                  ...weddingFeature.pole.directions[0]!.departures[0]!,
                  scheduled: soon,
                  predicted: soon,
                },
              ],
            },
          ],
        },
      ],
    };
    const nearby: NearbyResponse = {
      meta: {
        generatedAt: new Date().toISOString(),
        cell: 'u33dbjh',
        attribution: ['Data: VBB (CC BY 4.0)'],
        layers: {},
      },
      layers: { transit: { layer: 'transit', features: [liveStation] } },
    };
    const client = createApiClient('http://192.168.178.29:8080', {
      fetch: async () =>
        new Response(JSON.stringify(nearby), {
          status: 200,
          headers: { 'content-type': 'application/json' },
        }),
    });
    const queryClient = new QueryClient({ defaultOptions: { queries: { gcTime: Infinity } } });

    await render(
      <QueryClientProvider client={queryClient}>
        <MapScreen
          position={{ lat: 52.5427, lng: 13.366 }}
          source="live"
          baseUrl={{ ok: true, url: 'http://192.168.178.29:8080' }}
          client={client}
          store={createLocationStore()}
          devTools={false}
        />
      </QueryClientProvider>,
    );

    expect(map.props?.userInterfaceStyle).toBe('dark');
    const pin = await screen.findByTestId('marker');
    await fireEvent.press(pin);
    expect(sheet.props?.backgroundStyle).toEqual({ backgroundColor: themes.dark.colors.surface });
    expect(sheet.props?.handleIndicatorStyle).toEqual({
      backgroundColor: themes.dark.colors.handle,
    });
  });

  it('StatusBanner and LocationGate use the mode colours', async () => {
    currentScheme = 'dark';
    const clock = createClockStore(() => t0 + 6 * 60_000);
    await render(
      <StatusBanner
        checkedAt={t0}
        failure={null}
        onRetry={() => {}}
        retrying={false}
        clock={clock}
      />,
    );
    expect(screen.getByText('Updated 6 min ago')).toHaveStyle({
      color: themes.dark.colors.warningText,
    });
    expect(screen.getByText('Live times may be out of date')).toHaveStyle({
      color: themes.dark.colors.warningText,
    });

    const store = createLocationStore();
    store.getState().setPermission({ status: 'denied', canAskAgain: false });
    await render(<LocationGate store={store}>{() => null}</LocationGate>);
    expect(screen.getByText('Location is off')).toHaveStyle({ color: themes.dark.colors.text });
    expect(screen.getByText('Use map center')).toHaveStyle({
      color: themes.dark.colors.accentText,
    });
  });
});
