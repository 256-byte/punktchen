// Where the app looks (ARCHITECTURE §4.1 "location store"): which device
// fixes to keep, and what to show for each permission state. Pure: the
// expo-location glue lives in src/lib/location.ts.

import { distanceMeters, type LatLng } from './geo';

/** A device position. */
export interface Fix extends LatLng {
  /** Radius of 68 % confidence in metres; null if unknown. */
  accuracy: number | null;
  /** Device time of the fix (epoch ms). */
  at: number;
  /** lastKnown: cached by the OS, shown until the first live fix. */
  source: 'lastKnown' | 'live';
}

/** Smaller moves are GPS jitter: keeping them would only re-render. */
export const MIN_MOVE_M = 25;
/** The OS's cached position is used only if it is at most this old. */
export const LAST_KNOWN_MAX_AGE_MS = 5 * 60 * 1000;

/**
 * Whether a new fix replaces the current one: always the first and the first
 * live one; then a move of at least 25 m, or a fix at least twice as precise
 * (a coarse first fix gets refined while standing still).
 */
export function acceptFix(current: Fix | null, next: Fix): boolean {
  if (current === null || (current.source === 'lastKnown' && next.source === 'live')) {
    return true;
  }
  if (next.at < current.at) {
    return false; // late delivery of an older fix
  }
  if (distanceMeters(current, next) >= MIN_MOVE_M) {
    return true;
  }
  return (
    next.accuracy !== null && (current.accuracy === null || next.accuracy * 2 <= current.accuracy)
  );
}

/** Foreground location permission as the app sees it. */
export interface Permission {
  status: 'unknown' | 'granted' | 'denied';
  /** False once the OS no longer shows the prompt: only the settings app can grant it. */
  canAskAgain: boolean;
}

/** What to look around: the device position, or the map centre the user chose. */
export type LocationMode = 'device' | 'mapCenter';

/**
 * Where the map starts without a device position: Berlin Mitte
 * (Alexanderplatz), inside the area the app covers.
 */
export const BERLIN_CENTER: LatLng = { lat: 52.5219, lng: 13.4132 };

/**
 * The screen state:
 * - checking: permission or first fix pending (nothing to show yet),
 * - denied: friendly explanation with "allow" (or "open settings") and "use map center",
 * - ready: a position to fetch for.
 */
export type LocationView =
  | { kind: 'checking' }
  | { kind: 'denied'; canAskAgain: boolean }
  | { kind: 'ready'; position: LatLng; source: Fix['source'] | 'mapCenter' };

export function locationView(
  permission: Permission,
  mode: LocationMode,
  fix: Fix | null,
  mapCenter: LatLng,
): LocationView {
  if (mode === 'mapCenter') {
    return { kind: 'ready', position: mapCenter, source: 'mapCenter' };
  }
  switch (permission.status) {
    case 'unknown':
      return { kind: 'checking' };
    case 'denied':
      return { kind: 'denied', canAskAgain: permission.canAskAgain };
    case 'granted':
      return fix === null
        ? { kind: 'checking' }
        : { kind: 'ready', position: { lat: fix.lat, lng: fix.lng }, source: fix.source };
  }
}
