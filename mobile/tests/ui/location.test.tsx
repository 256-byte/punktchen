import { beforeEach, describe, expect, it, jest } from '@jest/globals';
import { act, fireEvent, render, renderHook, screen, waitFor } from '@testing-library/react-native';
import { Linking, Text } from 'react-native';

import { BERLIN_CENTER } from '@/core/location';
import { requestLocationAccess, useDeviceLocation } from '@/lib/location';
import { createLocationStore, type LocationStore } from '@/state/location';
import { LocationGate } from '@/ui/LocationGate';

import { expoLocationMock, fake, position } from './location-mock';

jest.mock(
  'expo-location',
  () => jest.requireActual<typeof import('./location-mock')>('./location-mock').expoLocationMock,
);

const t0 = Date.parse('2026-11-05T14:30:00+01:00');
// 0.0001° latitude ≈ 11.1 m.
const north = (m: number) => 52.5427 + (m / 11.12) * 0.0001;

let store: LocationStore;

beforeEach(() => {
  fake.reset();
  jest.clearAllMocks();
  store = createLocationStore();
});

function Screen() {
  useDeviceLocation(store);
  return (
    <LocationGate store={store}>
      {({ position: p, source }) => (
        <Text>
          ready {source} {p.lat.toFixed(4)},{p.lng.toFixed(4)}
        </Text>
      )}
    </LocationGate>
  );
}

describe('permission flow', () => {
  it('asks once, then shows the last known position and follows the device', async () => {
    fake.lastKnown = position(52.5427, 13.366, t0 - 60_000, 65);
    await render(<Screen />);

    expect(await screen.findByText('ready lastKnown 52.5427,13.3660')).toBeOnTheScreen();
    expect(expoLocationMock.requestForegroundPermissionsAsync).toHaveBeenCalledTimes(1);
    expect(expoLocationMock.getLastKnownPositionAsync).toHaveBeenCalledWith({ maxAge: 300_000 });
    await waitFor(() => expect(fake.watchers).toHaveLength(1));
    expect(expoLocationMock.watchPositionAsync.mock.calls[0]?.[0]).toEqual({
      accuracy: 3,
      distanceInterval: 25,
      timeInterval: 5000,
    });

    await act(async () => fake.emit(position(52.5427, 13.366, t0, 10)));
    expect(screen.getByText('ready live 52.5427,13.3660')).toBeOnTheScreen();
  });

  it('keeps the position through jitter, follows real moves (movement threshold)', async () => {
    fake.current = { status: 'granted', canAskAgain: true };
    await render(<Screen />);
    await waitFor(() => expect(fake.watchers).toHaveLength(1));
    expect(screen.getByText('Finding your location…')).toBeOnTheScreen();

    await act(async () => fake.emit(position(52.5427, 13.366, t0)));
    const kept = store.getState().fix;
    await act(async () => fake.emit(position(north(12), 13.366, t0 + 5000)));
    expect(store.getState().fix).toBe(kept);

    await act(async () => fake.emit(position(north(40), 13.366, t0 + 10_000)));
    expect(store.getState().fix?.lat).toBeCloseTo(north(40), 6);
    expect(screen.getByText(`ready live ${north(40).toFixed(4)},13.3660`)).toBeOnTheScreen();
    expect(expoLocationMock.requestForegroundPermissionsAsync).not.toHaveBeenCalled();
  });

  it('stops watching on unmount', async () => {
    fake.current = { status: 'granted', canAskAgain: true };
    const { unmount } = await renderHook(() => useDeviceLocation(store));
    await waitFor(() => expect(fake.watchers).toHaveLength(1));
    await unmount();
    expect(fake.watchers[0]?.removed).toBe(true);
  });
});

describe('denied permission', () => {
  it('explains it and offers the map center (acceptance criterion)', async () => {
    fake.onRequest = { status: 'denied', canAskAgain: true };
    await render(<Screen />);

    expect(await screen.findByText('Location is off')).toBeOnTheScreen();
    expect(screen.getByText(/stops around you/)).toBeOnTheScreen();
    expect(screen.getByRole('button', { name: 'Allow location' })).toBeOnTheScreen();
    expect(fake.watchers).toHaveLength(0);

    fireEvent.press(screen.getByRole('button', { name: 'Use map center' }));
    expect(
      await screen.findByText(
        `ready mapCenter ${BERLIN_CENTER.lat.toFixed(4)},${BERLIN_CENTER.lng.toFixed(4)}`,
      ),
    ).toBeOnTheScreen();
  });

  it('asks again from the screen and follows the device once granted', async () => {
    fake.onRequest = { status: 'denied', canAskAgain: true };
    fake.lastKnown = position(52.5427, 13.366, t0);
    await render(<Screen />);
    await screen.findByText('Location is off');

    fake.onRequest = { status: 'granted', canAskAgain: true };
    fireEvent.press(screen.getByRole('button', { name: 'Allow location' }));
    expect(await screen.findByText('ready lastKnown 52.5427,13.3660')).toBeOnTheScreen();
  });

  it('opens the settings app once the OS no longer asks', async () => {
    const openSettings = jest.spyOn(Linking, 'openSettings').mockResolvedValue();
    fake.current = { status: 'denied', canAskAgain: false };
    await render(<Screen />);

    expect(await screen.findByText(/Settings app/)).toBeOnTheScreen();
    fireEvent.press(screen.getByRole('button', { name: 'Open settings' }));
    await waitFor(() => expect(openSettings).toHaveBeenCalledTimes(1));
    expect(expoLocationMock.requestForegroundPermissionsAsync).not.toHaveBeenCalled();
  });
});

describe('requestLocationAccess', () => {
  it('switches back from the map center when granted', async () => {
    store.getState().setPermission({ status: 'denied', canAskAgain: true });
    store.getState().chooseMapCenter();
    await requestLocationAccess(store);
    expect(store.getState().permission).toEqual({ status: 'granted', canAskAgain: true });
    expect(store.getState().mode).toBe('device');
  });

  it('stays on the map center when denied again', async () => {
    fake.onRequest = { status: 'denied', canAskAgain: false };
    store.getState().setPermission({ status: 'denied', canAskAgain: true });
    store.getState().chooseMapCenter();
    await requestLocationAccess(store);
    expect(store.getState().permission).toEqual({ status: 'denied', canAskAgain: false });
    expect(store.getState().mode).toBe('mapCenter');
  });
});

describe('waiting for a position', () => {
  it('offers the map center while no fix arrives', async () => {
    fake.current = { status: 'granted', canAskAgain: true };
    await render(<Screen />);
    fireEvent.press(await screen.findByRole('button', { name: 'Use map center instead' }));
    expect(await screen.findByText(/^ready mapCenter/)).toBeOnTheScreen();
  });
});
