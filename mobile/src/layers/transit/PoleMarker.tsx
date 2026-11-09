import { useEffect, useMemo, useState } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { Marker } from 'react-native-maps';

import {
  badgeColor,
  type Direction,
  directionKey,
  markerDirections,
  nextLabels,
  textColorOn,
} from '@/core/directions';
import { type ClockStore, clockStore, useNow } from '@/state/clock';

import type { PoleFeature } from './index';

/** How long a marker redraws its native snapshot after its content changed. */
export const TRACK_CHANGES_MS = 500;

/**
 * A pole: one row per direction with line badge, headsign and the countdown
 * to the next departure (task 3.5). Re-renders when a label changes (about
 * once a minute), not on every tick (ARCHITECTURE §4.5).
 */
export function PoleMarker({
  feature,
  clock = clockStore,
}: {
  feature: PoleFeature;
  clock?: ClockStore;
}) {
  const { shown, more } = useMemo(
    () => markerDirections(feature.pole.directions),
    [feature.pole.directions],
  );
  const labels = useNow((now) => nextLabels(shown, now), clock);
  const tracks = useTracksViewChanges(feature.pole.directions, labels);

  return (
    <Marker
      coordinate={{ latitude: feature.pole.location.lat, longitude: feature.pole.location.lng }}
      anchor={{ x: 0.5, y: 1 }}
      tracksViewChanges={tracks}
    >
      <View style={styles.bubble} accessibilityLabel={accessibilityText(feature, shown, labels)}>
        {shown.map((d, i) => (
          <Row key={directionKey(d)} direction={d} label={labels[i] ?? null} />
        ))}
        {more > 0 ? <Text style={styles.more}>+{more} more</Text> : null}
      </View>
    </Marker>
  );
}

function Row({ direction, label }: { direction: Direction; label: string | null }) {
  const background = badgeColor(direction);
  return (
    <View style={[styles.row, label === null && styles.idle]}>
      <View style={[styles.badge, { backgroundColor: background }]}>
        <Text style={[styles.line, { color: textColorOn(background) }]}>{direction.line}</Text>
      </View>
      <Text style={styles.headsign} numberOfLines={1}>
        {direction.headsign}
      </Text>
      <Text style={styles.countdown}>{label ?? '—'}</Text>
    </View>
  );
}

function accessibilityText(feature: PoleFeature, shown: Direction[], labels: (string | null)[]) {
  const rows = shown.map(
    (d, i) => `${d.line} to ${d.headsign}: ${labels[i] ?? 'no departure soon'}`,
  );
  return [feature.station.name, ...rows].join('. ');
}

/**
 * tracksViewChanges for a custom marker (Google Maps; Apple Maps ignores
 * it): on after the content changed, off again shortly after, so the map
 * does not redraw the marker every frame (ARCHITECTURE §4.5).
 */
function useTracksViewChanges(directions: Direction[], labels: (string | null)[]): boolean {
  const content = useMemo(() => ({ directions, labels }), [directions, labels]);
  const [tracking, setTracking] = useState(true);
  const [seen, setSeen] = useState(content);
  if (seen !== content) {
    setSeen(content);
    setTracking(true);
  }
  useEffect(() => {
    if (!tracking) return;
    const timer = setTimeout(() => setTracking(false), TRACK_CHANGES_MS);
    return () => clearTimeout(timer);
  }, [tracking, content]);
  return tracking;
}

const styles = StyleSheet.create({
  bubble: {
    minWidth: 120,
    maxWidth: 220,
    paddingVertical: 4,
    paddingHorizontal: 6,
    borderRadius: 8,
    backgroundColor: '#ffffff',
    gap: 3,
    shadowColor: '#000000',
    shadowOpacity: 0.2,
    shadowRadius: 3,
    shadowOffset: { width: 0, height: 1 },
    elevation: 3,
  },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
  },
  idle: {
    opacity: 0.5,
  },
  badge: {
    minWidth: 30,
    paddingHorizontal: 4,
    paddingVertical: 1,
    borderRadius: 4,
    alignItems: 'center',
  },
  line: {
    fontSize: 12,
    fontWeight: '700',
  },
  headsign: {
    flexShrink: 1,
    fontSize: 12,
    color: '#111111',
  },
  countdown: {
    marginLeft: 'auto',
    fontSize: 12,
    fontWeight: '600',
    fontVariant: ['tabular-nums'],
    color: '#111111',
  },
  more: {
    fontSize: 11,
    color: '#555555',
  },
});
