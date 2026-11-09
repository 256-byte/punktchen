// Location store (ARCHITECTURE §4.1): permission, the kept device fix and
// the chosen mode. Written by src/lib/location.ts (device) and the map
// (centre, task 3.4); read through useLocationView.
import { useMemo } from 'react';
import { useStore } from 'zustand';
import { useShallow } from 'zustand/react/shallow';
import { createStore } from 'zustand/vanilla';

import type { LatLng } from '@/core/geo';
import {
  acceptFix,
  BERLIN_CENTER,
  type Fix,
  type LocationMode,
  type LocationView,
  locationView,
  type Permission,
} from '@/core/location';

export interface LocationState {
  permission: Permission;
  fix: Fix | null;
  mode: LocationMode;
  mapCenter: LatLng;
  /** The last place chosen with lookAt; the map moves there once per seq. */
  jump: { center: LatLng; seq: number } | null;
  setPermission(permission: Permission): void;
  /** Keeps the fix only if acceptFix says so (movement threshold). */
  offerFix(fix: Fix): void;
  /** "Use map center": look around mapCenter instead of the device. */
  chooseMapCenter(): void;
  /** Back to the device position (e.g. after granting the permission). */
  chooseDevice(): void;
  setMapCenter(center: LatLng): void;
  /** Look around a chosen place (dev place picker): map-centre mode there, the map moves. */
  lookAt(center: LatLng): void;
}

export function createLocationStore() {
  return createStore<LocationState>()((set) => ({
    permission: { status: 'unknown', canAskAgain: true },
    fix: null,
    mode: 'device',
    mapCenter: BERLIN_CENTER,
    jump: null,
    setPermission: (permission) => set({ permission }),
    offerFix: (fix) => set((s) => (acceptFix(s.fix, fix) ? { fix } : s)),
    chooseMapCenter: () => set({ mode: 'mapCenter' }),
    chooseDevice: () => set({ mode: 'device' }),
    setMapCenter: (mapCenter) => set({ mapCenter }),
    lookAt: (center) =>
      set((s) => ({
        mode: 'mapCenter',
        mapCenter: center,
        jump: { center, seq: (s.jump?.seq ?? 0) + 1 },
      })),
  }));
}

export type LocationStore = ReturnType<typeof createLocationStore>;

/** The app's store. Tests create their own with createLocationStore. */
export const locationStore = createLocationStore();

/** What to show (checking / denied / ready with a position); re-renders only when it changes. */
export function useLocationView(store: LocationStore = locationStore): LocationView {
  const { permission, mode, fix, mapCenter } = useStore(
    store,
    useShallow((s) => ({
      permission: s.permission,
      mode: s.mode,
      fix: s.fix,
      mapCenter: s.mapCenter,
    })),
  );
  return useMemo(
    () => locationView(permission, mode, fix, mapCenter),
    [permission, mode, fix, mapCenter],
  );
}
