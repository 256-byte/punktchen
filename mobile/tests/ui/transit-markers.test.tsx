import { afterEach, beforeEach, describe, expect, it, jest } from '@jest/globals';
import { act, fireEvent, render, renderHook, screen, within } from '@testing-library/react-native';
import { Profiler } from 'react';

import type { NearbyResponse } from '@/api/nearby';
import type { Departure } from '@/core/departures';
import type { Direction } from '@/core/directions';
import { type PoleFeature, transitLayer } from '@/layers/transit';
import { themes } from '@/ui/tokens';
import { PoleMarker, TRACK_CHANGES_MS } from '@/layers/transit/PoleMarker';
import { type ClockStore, createClockStore, useClockTicker } from '@/state/clock';

jest.mock(
  'react-native-maps',
  () => jest.requireActual<typeof import('./maps-mock')>('./maps-mock').mapsMock,
);

const t0 = Date.parse('2026-11-05T14:30:00+01:00');

function dep(time: string): Departure {
  return {
    tripId: `t-${time}`,
    serviceDate: '2026-11-05',
    scheduled: `2026-11-05T${time}:00+01:00`,
    predicted: null,
    delaySeconds: null,
    source: 'scheduled',
    cancelled: false,
  };
}

function direction(line: string, headsign: string, departures: Departure[]): Direction {
  return {
    line,
    mode: 'bus',
    color: null,
    headsign,
    isTerminal: false,
    isRing: false,
    departures,
  };
}

function feature(directions: Direction[]): PoleFeature {
  return {
    station: {
      id: 'de:11000:900009104',
      kind: 'station',
      name: 'S+U Wedding',
      location: { lat: 52.5427, lng: 13.366 },
      modes: ['bus'],
      poles: [],
    },
    pole: {
      id: 'de:11000:900009104::3',
      location: { lat: 52.5426, lng: 13.3658 },
      platform: null,
      description: null,
      directions,
    },
  };
}

/** A clock the test moves by hand. */
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
});

afterEach(() => {
  jest.useRealTimers();
});

describe('PoleMarker', () => {
  const f = feature([
    direction('M27', 'S Jungfernheide', [dep('14:33'), dep('14:43')]),
    direction('247', 'S Nordbahnhof', [dep('15:45')]),
    direction('N20', 'S Oranienburger Str.', []),
    direction('120', 'U Leopoldplatz', [dep('14:31')]),
    direction('142', 'S Ostbahnhof', [dep('14:40')]),
  ]);

  it('shows line, headsign and countdown of the running directions', async () => {
    await render(<PoleMarker feature={f} clock={clock} />);
    expect(screen.getByText('M27')).toBeOnTheScreen();
    expect(screen.getByText('S Jungfernheide')).toBeOnTheScreen();
    expect(screen.getByText('3 min')).toBeOnTheScreen();
    expect(screen.getByText('15:45')).toBeOnTheScreen(); // ≥ 60 min: clock time
    expect(screen.getByText('1 min')).toBeOnTheScreen();
    expect(screen.getByText('142')).toBeOnTheScreen(); // 4 running rows fit
    expect(screen.queryByText('N20')).toBeNull(); // nothing soon (night line): sheet only
    expect(screen.queryByText('—')).toBeNull();
    expect(screen.queryByText(/more$/)).toBeNull();
    // Soonest departure first (task 7.7), not API order.
    const markerButton = screen.getByLabelText(
      'S+U Wedding, Bus. 120 to U Leopoldplatz: 1 min, timetable. ' +
        'M27 to S Jungfernheide: 3 min, timetable. 142 to S Ostbahnhof: 10 min, timetable. ' +
        '247 to S Nordbahnhof: 15:45, timetable',
    );
    expect(markerButton).toBeOnTheScreen();
    expect(markerButton.props.accessible).toBe(true);
    expect(markerButton.props.accessibilityRole).toBe('button');
    expect(markerButton.props.accessibilityHint).toBe('Opens station departures');
  });

  it('colours live countdowns and shows the delay of late ones (task 4.5)', async () => {
    const live = (time: string, predicted: string, delaySeconds: number): Departure => ({
      ...dep(time),
      predicted: `2026-11-05T${predicted}:00+01:00`,
      delaySeconds,
      source: 'realtime',
    });
    const rt = feature([
      direction('M27', 'S Jungfernheide', [live('14:33', '14:35', 120)]),
      direction('120', 'U Leopoldplatz', [live('14:36', '14:36', 20)]),
      direction('247', 'S Nordbahnhof', [dep('14:38')]),
    ]);
    await render(<PoleMarker feature={rt} clock={clock} />);

    const late = screen.getByLabelText('5 min, live, 2 min late'); // by its predicted time
    expect(late).toHaveTextContent('5 min +2');
    expect(late).toHaveStyle({ color: themes.light.colors.late });
    expect(screen.getByLabelText('6 min, live, on time')).toHaveStyle({
      color: themes.light.colors.live,
    });
    const timetable = screen.getByLabelText('8 min, timetable');
    expect(timetable).toHaveTextContent('8 min');
    expect(timetable).toHaveStyle({ color: themes.light.colors.text });
  });

  it('counts the running directions that do not fit', async () => {
    const busy = feature([
      ...f.pole.directions,
      direction('N40', 'S Karlshorst', []),
      direction('147', 'S Ostbahnhof', [dep('14:50')]),
    ]);
    await render(<PoleMarker feature={busy} clock={clock} />);
    expect(screen.getByText('+1 more')).toBeOnTheScreen();
    expect(screen.queryByText('N40')).toBeNull();
    expect(screen.getByLabelText(/\. 1 more direction$/)).toBeOnTheScreen();
  });

  it('draws at displayAt when moved aside and reports its size (task 7.9)', async () => {
    const onSize = jest.fn();
    await render(
      <PoleMarker
        feature={f}
        clock={clock}
        displayAt={{ lat: 52.55, lng: 13.37 }}
        onSize={onSize}
      />,
    );
    expect(screen.getByTestId('marker').props.coordinate).toEqual({
      latitude: 52.55,
      longitude: 13.37,
    });
    await act(async () =>
      fireEvent(screen.getByLabelText(/^S\+U Wedding, Bus\. 120/, { exact: false }), 'layout', {
        nativeEvent: { layout: { x: 0, y: 0, width: 180, height: 96 } },
      }),
    );
    expect(onSize).toHaveBeenLastCalledWith({ width: 180, height: 96 });

    // Nothing to draw: takes no room.
    const night = feature([direction('N20', 'S Oranienburger Str.', [])]);
    await render(<PoleMarker feature={night} clock={clock} onSize={onSize} />);
    expect(onSize).toHaveBeenLastCalledWith({ width: 0, height: 0 });
  });

  it('draws no marker for a pole with nothing coming', async () => {
    const night = feature([direction('N20', 'S Oranienburger Str.', [])]);
    await render(<PoleMarker feature={night} clock={clock} />);
    expect(screen.queryByTestId('marker')).toBeNull();
  });

  it('drops a direction once its last departure has left', async () => {
    const one = feature([
      direction('120', 'U Leopoldplatz', [dep('14:31')]),
      direction('M27', 'S Jungfernheide', [dep('14:40')]),
    ]);
    await render(<PoleMarker feature={one} clock={clock} />);
    expect(screen.getByText('120')).toBeOnTheScreen();
    await at(t0 + 2 * 60_000); // 14:31 has left
    expect(screen.queryByText('120')).toBeNull();
    expect(screen.getByText('M27')).toBeOnTheScreen();
  });

  it('keeps the rows ordered by next departure as the clock moves (task 7.7)', async () => {
    const lines = () => screen.getAllByText(/^(M27|247|120|142)$/).map((n) => n.props.children);
    await render(<PoleMarker feature={f} clock={clock} />);
    expect(lines()).toEqual(['120', 'M27', '142', '247']); // 14:31, 14:33, 14:40, 15:45
    await at(t0 + 4 * 60_000); // 14:34: 120 and M27 14:33 have left; M27 next 14:43
    expect(lines()).toEqual(['142', 'M27', '247']); // 14:40, 14:43, 15:45
  });

  it('draws a combined stop as sections divided by a line (task 7.8)', async () => {
    const sbahn = (line: string, time: string): Direction => ({
      ...direction(line, line === 'S15' ? 'S+U Hauptbahnhof' : `Ringbahn ${line}`, [dep(time)]),
      mode: 'sbahn',
      isRing: line !== 'S15',
    });
    const gleis1 = {
      ...f.pole,
      id: 'g1',
      platform: '1',
      directions: [sbahn('S41', '14:36'), sbahn('S15', '14:31')],
    };
    const gleis2 = {
      ...f.pole,
      id: 'g2',
      platform: '2',
      directions: [sbahn('S42', '14:33'), sbahn('S15', '14:40')],
    };
    const combined: PoleFeature = {
      ...f,
      pole: { ...f.pole, id: 'g1+g2', directions: [...gleis1.directions, ...gleis2.directions] },
      poleCount: 2,
      sections: [gleis1, gleis2],
    };
    await render(<PoleMarker feature={combined} clock={clock} />);
    const lineIn = (id: string) =>
      within(screen.getByTestId(id))
        .getAllByText(/^S\d+$/)
        .map((n) => n.props.children as string);
    expect(lineIn('section-0')).toEqual(['S15', 'S41']); // soonest first
    expect(lineIn('section-1')).toEqual(['S42', 'S15']);
    // Same row spacing in every section (the first one too); a line above all but the first.
    expect(screen.getByTestId('section-0')).toHaveStyle({ gap: 3 });
    expect(screen.getByTestId('section-0')).not.toHaveStyle({
      borderTopColor: themes.light.colors.border,
    });
    expect(screen.getByTestId('section-1')).toHaveStyle({
      gap: 3,
      borderTopColor: themes.light.colors.border,
    });
    expect(
      screen.getByLabelText(/^S\+U Wedding, S-Bahn\. S15 to S\+U Hauptbahnhof: 1 min/),
    ).toBeOnTheScreen();
  });

  it('counts down with the clock and moves on to the next departure', async () => {
    await render(<PoleMarker feature={f} clock={clock} />);
    await at(t0 + 60_000);
    expect(screen.getByText('2 min')).toBeOnTheScreen();
    await at(t0 + 3 * 60_000); // M27 14:33 is due
    expect(screen.getAllByText('now').length).toBeGreaterThan(0);
    await at(t0 + 4 * 60_000); // 14:33 has left: next is 14:43
    expect(screen.getByText('9 min')).toBeOnTheScreen();
  });

  it('re-renders once a minute, not every second', async () => {
    jest.useFakeTimers();
    // Minutes are rounded down: at 14:30:01 the 14:33 departure turned "2 min".
    await at(t0 + 1000);
    const onRender = jest.fn();
    await render(
      <Profiler id="marker" onRender={onRender}>
        <PoleMarker feature={f} clock={clock} />
      </Profiler>,
    );
    // Let the first tracksViewChanges period end: that render is not a tick.
    await act(async () => jest.advanceTimersByTime(TRACK_CHANGES_MS));
    onRender.mockClear();

    for (let s = 2; s <= 60; s++) await at(t0 + s * 1000);
    expect(onRender).not.toHaveBeenCalled();

    await at(t0 + 61_000); // 14:31:01: "2 min" → "1 min", "1 min" (14:31) → "now"
    expect(onRender).toHaveBeenCalledTimes(1);
  });

  it('tracks view changes only briefly after its content changed', async () => {
    jest.useFakeTimers();
    await render(<PoleMarker feature={f} clock={clock} />);
    const tracks = () => screen.getByTestId('marker').props.tracksViewChanges as boolean;
    expect(tracks()).toBe(true);

    await act(async () => jest.advanceTimersByTime(TRACK_CHANGES_MS));
    expect(tracks()).toBe(false);

    await at(t0 + 60_000);
    expect(tracks()).toBe(true);
    await act(async () => jest.advanceTimersByTime(TRACK_CHANGES_MS));
    expect(tracks()).toBe(false);
  });

  it('shows the station header and cluster count on a multi-pole station cluster (task 7.2)', async () => {
    const clustered: PoleFeature = {
      ...f,
      station: { ...f.station, name: 'S+U Alexanderplatz' },
      poleCount: 8,
      stationCount: 3,
      detail: 'stations',
    };
    await render(<PoleMarker feature={clustered} clock={clock} />);
    expect(screen.getByText('S+U Alexanderplatz')).toBeOnTheScreen();
    expect(screen.getByText('+2')).toBeOnTheScreen();
    expect(
      screen.getByLabelText(
        /^S\+U Alexanderplatz and 2 nearby stops, Bus\. 120 to U Leopoldplatz: 1 min, timetable\./,
      ),
    ).toBeOnTheScreen();
  });

  it('switches to line badges without headsigns or countdowns at compact zoom (task 7.2)', async () => {
    const compactFeature: PoleFeature = {
      ...f,
      station: { ...f.station, name: 'S+U Alexanderplatz' },
      pole: {
        ...f.pole,
        directions: [...f.pole.directions, direction('U6', 'U Alt-Mariendorf', [dep('14:35')])],
      },
      poleCount: 6,
      stationCount: 2,
      detail: 'compact',
    };
    await render(<PoleMarker feature={compactFeature} clock={clock} />);
    expect(screen.getByText('S+U Alexanderplatz')).toBeOnTheScreen();
    expect(screen.getAllByText('+1')).toHaveLength(2);
    expect(screen.getByText('M27')).toBeOnTheScreen();
    expect(screen.getByText('247')).toBeOnTheScreen();
    // 5 running lines (M27, 247, 120, 142, U6): 4 badges + "+1".
    expect(
      screen.getByLabelText(
        'S+U Alexanderplatz and 1 nearby stops, Bus. Lines: M27, 247, 120, 142, and 1 more',
      ),
    ).toBeOnTheScreen();
    // No headsigns or countdowns at compact zoom.
    expect(screen.queryByText('S Jungfernheide')).toBeNull();
    expect(screen.queryByText('3 min')).toBeNull();
  });

  const headerIcons = () =>
    within(screen.getByTestId('marker-header'))
      .queryAllByTestId(/^mode-icon-/)
      .map((n) => n.props.testID);

  it('shows the bus icon next to the name of a bus stop at street zoom (task 7.10)', async () => {
    await render(<PoleMarker feature={f} clock={clock} />);
    expect(headerIcons()).toEqual(['mode-icon-bus']);
    expect(within(screen.getByTestId('marker-header')).getByText('S+U Wedding')).toBeOnTheScreen();
  });

  it.each(['poles', 'stations', 'compact'] as const)(
    'shows every mode of a merged marker, rail first, at %s zoom (task 7.10)',
    async (detail) => {
      const hub: PoleFeature = {
        ...feature([
          direction('120', 'U Leopoldplatz', [dep('14:31')]),
          { ...direction('U6', 'U Alt-Mariendorf', [dep('14:33')]), mode: 'ubahn' },
          { ...direction('S41', 'Ringbahn S 41', [dep('14:35')]), mode: 'sbahn', isRing: true },
        ]),
        poleCount: 3,
        detail,
      };
      await render(<PoleMarker feature={hub} clock={clock} />);
      expect(headerIcons()).toEqual(['mode-icon-sbahn', 'mode-icon-ubahn', 'mode-icon-bus']);
      expect(screen.getByLabelText(/^S\+U Wedding, S-Bahn, U-Bahn, Bus\./)).toBeOnTheScreen();
    },
  );

  it('shows ⟳/⟲ on Ringbahn lines and hides terminal arrival directions (task 7.3)', async () => {
    const ringAndTerminal = feature([
      {
        ...direction('S41', 'Ringbahn S 41', [dep('14:32')]),
        mode: 'sbahn',
        isRing: true,
      },
      {
        ...direction('S42', 'Ringbahn S 42', [dep('14:34')]),
        mode: 'sbahn',
        isRing: true,
      },
      {
        ...direction('U6', 'S+U Wedding', [dep('14:31')]),
        mode: 'ubahn',
        isTerminal: true,
      },
      {
        ...direction('U6', 'U Alt-Mariendorf', [dep('14:35')]),
        mode: 'ubahn',
      },
    ]);
    await render(<PoleMarker feature={ringAndTerminal} clock={clock} />);
    expect(screen.getByText('⟳ Ringbahn S 41')).toBeOnTheScreen();
    expect(screen.getByText('⟲ Ringbahn S 42')).toBeOnTheScreen();
    expect(screen.getByText('U Alt-Mariendorf')).toBeOnTheScreen();
    // The terminal arrival "→ S+U Wedding" has no row; the name is only in the header.
    expect(screen.getAllByText('S+U Wedding')).toHaveLength(1);
    expect(
      screen.getByLabelText(
        /^S\+U Wedding, S-Bahn, U-Bahn\. S41 to ⟳ Ringbahn S 41: 2 min, timetable\. S42 to ⟲ Ringbahn S 42: 4 min, timetable\. U6 to U Alt-Mariendorf: 5 min, timetable$/,
      ),
    ).toBeOnTheScreen();
  });
});

describe('transit layer', () => {
  it('draws one marker per pole with something coming (no shared line: not one stop)', () => {
    const station = feature([]).station;
    const m27 = feature([direction('M27', 'S Jungfernheide', [dep('14:33')])]).pole;
    const response = {
      meta: { generatedAt: '2026-11-05T14:30:12+01:00', attribution: [], layers: {} },
      layers: {
        transit: {
          layer: 'transit',
          features: [
            {
              ...station,
              poles: [
                m27,
                {
                  ...m27,
                  id: 'de:11000:900009104::4',
                  directions: [direction('247', 'S Nordbahnhof', [dep('14:35')])],
                },
                { ...m27, id: 'de:11000:900009104::9', directions: [] }, // nothing: no marker
              ],
            },
          ],
        },
      },
    } as NearbyResponse;
    expect(transitLayer.select(response).map((p) => transitLayer.key(p))).toEqual([
      'de:11000:900009104::3',
      'de:11000:900009104::4',
    ]);
  });
});

describe('clock', () => {
  it('ticks every second while mounted, with the server offset', async () => {
    jest.useFakeTimers();
    await renderHook(() => useClockTicker(clock));
    expect(clock.getState().now).toBe(t0);

    deviceNow = t0 + 1000;
    await act(async () => jest.advanceTimersByTime(1000));
    expect(clock.getState().now).toBe(t0 + 1000);

    await act(async () => clock.getState().setOffset(-120_000));
    expect(clock.getState().now).toBe(t0 + 1000 - 120_000);
    deviceNow = t0 + 2000;
    await act(async () => jest.advanceTimersByTime(1000));
    expect(clock.getState().now).toBe(t0 + 2000 - 120_000);
  });

  it('ignores an unchanged offset', () => {
    const before = clock.getState();
    clock.getState().setOffset(0);
    expect(clock.getState()).toBe(before);
  });
});
