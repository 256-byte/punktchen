import { useEffect, useMemo, useState } from 'react';
import { type LayoutChangeEvent, View } from 'react-native';
import { Marker } from 'react-native-maps';

import type { LatLng } from '@/core/geo';
import type { Size } from '@/core/labelLayout';
import { opacity } from '@/ui/tokens';

import { AmenityIcon, type AmenityKind } from './AmenityIcon';

/** Long enough for Google Maps to take a snapshot of the new content. */
const TRACK_CHANGES_MS = 500;

/**
 * Below the transit bubbles (they carry departures) but above the map.
 * PoleMarker uses TRANSIT_Z_INDEX.
 */
export const AMENITY_Z_INDEX = 1;

/**
 * A toilet or fountain on the map: a small round icon, greyed when it
 * cannot be used now (fountain off season or out of service, toilet only
 * open at events; user decision 2026-11-18). Kept apart from other markers
 * like the transit bubbles (task 7.9): it reports its size and is drawn at
 * displayAt when moved.
 */
export function AmenityMarker({
  kind,
  at,
  displayAt,
  dimmed,
  label,
  onPress,
  onSize,
}: {
  kind: AmenityKind;
  at: LatLng;
  displayAt?: LatLng;
  dimmed: boolean;
  label: string;
  onPress?: () => void;
  onSize?: (size: Size) => void;
}) {
  const point = displayAt ?? at;
  const coordinate = useMemo(
    () => ({ latitude: point.lat, longitude: point.lng }),
    [point.lat, point.lng],
  );
  const tracks = useTracksViewChanges(dimmed);
  const reportLayout = (e: LayoutChangeEvent) => {
    const { width, height } = e.nativeEvent.layout;
    onSize?.({ width, height });
  };
  return (
    <Marker
      testID="amenity-marker"
      coordinate={coordinate}
      anchor={{ x: 0.5, y: 0.5 }}
      zIndex={AMENITY_Z_INDEX}
      tracksViewChanges={tracks}
      accessibilityLabel={label}
      onPress={onPress}
    >
      <View
        accessible
        accessibilityRole="button"
        accessibilityLabel={label}
        accessibilityHint="Opens details"
        onLayout={reportLayout}
        style={dimmed ? { opacity: opacity.dimmed } : undefined}
        testID={`amenity-${kind}${dimmed ? '-dimmed' : ''}`}
      >
        <AmenityIcon kind={kind} />
      </View>
    </Marker>
  );
}

/** Tracks view changes for a moment after mounting and after `dimmed` flips. */
function useTracksViewChanges(dimmed: boolean): boolean {
  const [tracking, setTracking] = useState(true);
  const [seen, setSeen] = useState(dimmed);
  if (seen !== dimmed) {
    setSeen(dimmed);
    setTracking(true);
  }
  useEffect(() => {
    if (!tracking) return;
    const timer = setTimeout(() => setTracking(false), TRACK_CHANGES_MS);
    return () => clearTimeout(timer);
  }, [tracking]);
  return tracking;
}
