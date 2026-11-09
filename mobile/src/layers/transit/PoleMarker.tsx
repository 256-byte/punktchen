import { useEffect, useMemo, useState } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { Marker } from 'react-native-maps';

import {
  badgeColor,
  type Direction,
  directionKey,
  type MarkerRow,
  markerRows,
  nextLabels,
  textColorOn,
} from '@/core/directions';
import { type ClockStore, clockStore, useNow } from '@/state/clock';

import type { PoleFeature } from './index';

/** How long a marker redraws its native snapshot after its content changed. */
export const TRACK_CHANGES_MS = 500;

/**
 * A pole: one row per direction with an upcoming departure — line badge,
 * headsign, countdown (task 3.5). Directions with nothing coming are left
 * out (station sheet, 3.6); a pole with none draws no marker. Re-renders
 * when a label changes (about once a minute), not on every tick
 * (ARCHITECTURE §4.5).
 */
export function PoleMarker({
  feature,
  clock = clockStore,
}: {
  feature: PoleFeature;
  clock?: ClockStore;
}) {
  const directions = feature.pole.directions;
  const labels = useNow((now) => nextLabels(directions, now), clock);
  const { rows, more } = useMemo(() => markerRows(directions, labels), [directions, labels]);
  const tracks = useTracksViewChanges(directions, labels);

  if (rows.length === 0) {
    return null;
  }
  return (
    <Marker
      coordinate={{ latitude: feature.pole.location.lat, longitude: feature.pole.location.lng }}
      anchor={{ x: 0.5, y: 1 }}
      tracksViewChanges={tracks}
    >
      <View style={styles.bubble} accessibilityLabel={accessibilityText(feature, rows)}>
        {rows.map((r) => (
          <Row key={directionKey(r.direction)} row={r} />
        ))}
        {more > 0 ? <Text style={styles.more}>+{more} more</Text> : null}
      </View>
    </Marker>
  );
}

function Row({ row: { direction, label } }: { row: MarkerRow }) {
  const background = badgeColor(direction);
  return (
    <View style={styles.row}>
      <View style={[styles.badge, { backgroundColor: background }]}>
        <Text style={[styles.line, { color: textColorOn(background) }]}>{direction.line}</Text>
      </View>
      <Text style={styles.headsign} numberOfLines={1}>
        {direction.headsign}
      </Text>
      <Text style={styles.countdown}>{label}</Text>
    </View>
  );
}

function accessibilityText(feature: PoleFeature, rows: MarkerRow[]) {
  const parts = rows.map((r) => `${r.direction.line} to ${r.direction.headsign}: ${r.label}`);
  return [feature.station.name, ...parts].join('. ');
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
