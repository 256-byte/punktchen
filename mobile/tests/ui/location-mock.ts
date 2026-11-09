// In-memory stand-in for expo-location (jest.mock factory) that tests drive:
// set the permission answers, the cached position, and emit watch updates.
import { jest } from '@jest/globals';
import type * as Location from 'expo-location';

type Response = { status: 'granted' | 'denied' | 'undetermined'; canAskAgain: boolean };
type Callback = (o: Location.LocationObject) => void;

export const fake = {
  current: { status: 'undetermined', canAskAgain: true } as Response,
  /** What the OS prompt answers. */
  onRequest: { status: 'granted', canAskAgain: true } as Response,
  lastKnown: null as Location.LocationObject | null,
  watchers: [] as { callback: Callback; removed: boolean }[],
  reset() {
    this.current = { status: 'undetermined', canAskAgain: true };
    this.onRequest = { status: 'granted', canAskAgain: true };
    this.lastKnown = null;
    this.watchers = [];
  },
  emit(o: Location.LocationObject) {
    for (const w of this.watchers) if (!w.removed) w.callback(o);
  },
};

export function position(lat: number, lng: number, at: number, accuracy = 15) {
  return {
    coords: {
      latitude: lat,
      longitude: lng,
      accuracy,
      altitude: null,
      altitudeAccuracy: null,
      heading: null,
      speed: null,
    },
    timestamp: at,
  } as Location.LocationObject;
}

export const expoLocationMock = {
  PermissionStatus: { GRANTED: 'granted', DENIED: 'denied', UNDETERMINED: 'undetermined' },
  Accuracy: { Balanced: 3 },
  getForegroundPermissionsAsync: jest.fn(async () => ({ ...fake.current })),
  requestForegroundPermissionsAsync: jest.fn(async () => {
    fake.current = { ...fake.onRequest };
    return { ...fake.current };
  }),
  getLastKnownPositionAsync: jest.fn(async (_options?: unknown) => fake.lastKnown),
  watchPositionAsync: jest.fn(async (_options: unknown, callback: Callback) => {
    const w = { callback, removed: false };
    fake.watchers.push(w);
    return { remove: () => (w.removed = true) };
  }),
};
