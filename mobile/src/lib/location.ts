// expo-location glue for the location store (task 3.3). Reads the permission
// on start and whenever the app returns to the foreground (the user may have
// changed it in the settings app), and follows the device position only
// while the app is in the foreground.
import * as Location from 'expo-location';
import { useEffect, useState } from 'react';
import { AppState, Linking } from 'react-native';
import { useStore } from 'zustand';

import { type Fix, LAST_KNOWN_MAX_AGE_MS, MIN_MOVE_M, type Permission } from '@/core/location';
import { locationStore, type LocationStore } from '@/state/location';

/** The OS reports at most one position per this interval while watching. */
const WATCH_INTERVAL_MS = 5000;

export function toPermission(r: Location.LocationPermissionResponse): Permission {
  switch (r.status) {
    case Location.PermissionStatus.GRANTED:
      return { status: 'granted', canAskAgain: r.canAskAgain };
    case Location.PermissionStatus.DENIED:
      return { status: 'denied', canAskAgain: r.canAskAgain };
    default:
      return { status: 'unknown', canAskAgain: r.canAskAgain };
  }
}

export function toFix(o: Location.LocationObject, source: Fix['source']): Fix {
  return {
    lat: o.coords.latitude,
    lng: o.coords.longitude,
    accuracy: o.coords.accuracy,
    at: o.timestamp,
    source,
  };
}

function applyPermission(store: LocationStore, next: Permission) {
  const { permission, setPermission, chooseDevice } = store.getState();
  setPermission(next);
  if (permission.status !== 'granted' && next.status === 'granted') {
    chooseDevice(); // granted after "use map center": follow the device again
  }
}

/**
 * Keeps the store's permission and device fix current. Mount once, near the
 * root. Asks for the permission once when the OS has not asked yet.
 */
export function useDeviceLocation(store: LocationStore = locationStore): void {
  const granted = useStore(store, (s) => s.permission.status === 'granted');
  const [active, setActive] = useState(AppState.currentState !== 'background');

  useEffect(() => {
    const sub = AppState.addEventListener('change', (state) => setActive(state === 'active'));
    return () => sub.remove();
  }, []);

  useEffect(() => {
    if (!active) {
      return;
    }
    let cancelled = false;
    void (async () => {
      let r = await Location.getForegroundPermissionsAsync();
      if (r.status === Location.PermissionStatus.UNDETERMINED && r.canAskAgain) {
        r = await Location.requestForegroundPermissionsAsync();
      }
      if (!cancelled) {
        applyPermission(store, toPermission(r));
      }
    })().catch((err: unknown) => {
      console.warn('location permission check failed', err);
    });
    return () => {
      cancelled = true;
    };
  }, [active, store]);

  useEffect(() => {
    if (!active || !granted) {
      return;
    }
    let stopped = false;
    let subscription: Location.LocationSubscription | null = null;
    const offer = (fix: Fix) => {
      if (!stopped) store.getState().offerFix(fix);
    };
    void (async () => {
      // The OS's cached position, if recent: something to show at once.
      try {
        const last = await Location.getLastKnownPositionAsync({ maxAge: LAST_KNOWN_MAX_AGE_MS });
        if (last) offer(toFix(last, 'lastKnown'));
      } catch (err) {
        console.warn('last known position failed', err);
      }
      if (stopped) return;
      const sub = await Location.watchPositionAsync(
        {
          accuracy: Location.Accuracy.Balanced,
          distanceInterval: MIN_MOVE_M,
          timeInterval: WATCH_INTERVAL_MS,
        },
        (o) => offer(toFix(o, 'live')),
      );
      if (stopped) sub.remove();
      else subscription = sub;
    })().catch((err: unknown) => {
      // E.g. location services off: the screen stays on "finding your
      // location", which offers the map centre.
      console.warn('watching the position failed', err);
    });
    return () => {
      stopped = true;
      subscription?.remove();
    };
  }, [active, granted, store]);
}

/**
 * "Allow location": shows the OS prompt again if it still can, else opens
 * the app's page in the settings app (useDeviceLocation picks the change up
 * when the app returns).
 */
export async function requestLocationAccess(store: LocationStore = locationStore): Promise<void> {
  if (!store.getState().permission.canAskAgain) {
    await Linking.openSettings();
    return;
  }
  applyPermission(store, toPermission(await Location.requestForegroundPermissionsAsync()));
}
