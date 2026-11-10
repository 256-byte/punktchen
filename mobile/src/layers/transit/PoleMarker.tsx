import { useEffect, useMemo, useState } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { Marker } from 'react-native-maps';

import {
  type Direction,
  directionKey,
  type MarkerRow,
  markerRows,
  nextCells,
} from '@/core/directions';
import { cellDescription } from '@/core/realtime';
import { type ClockStore, clockStore, useNow } from '@/state/clock';
import { themedStyles, useTheme } from '@/ui/theme';
import type { ColorScheme } from '@/ui/tokens';

import { DepartureTime } from './DepartureTime';
import type { PoleFeature } from './index';
import { LineBadge } from './LineBadge';

/** How long a marker redraws its native snapshot after its content changed. */
export const TRACK_CHANGES_MS = 500;

/**
 * A pole: one row per direction with an upcoming departure — line badge,
 * headsign, countdown (task 3.5) coloured live or timetable, with its
 * delay (4.5). Directions with nothing coming are left out (station sheet,
 * 3.6); a pole with none draws no marker. Re-renders when a cell changes
 * (about once a minute), not on every tick (ARCHITECTURE §4.5). With stale
 * data live countdowns are greyed out (6.2). Colours follow light/dark
 * mode (7.1).
 */
export function PoleMarker({
  feature,
  onPress,
  stale = false,
  clock = clockStore,
}: {
  feature: PoleFeature;
  /** The data is older than 5 min (ARCHITECTURE §4.3). */
  stale?: boolean;
  /** Tap: open the station sheet (task 3.6). */
  onPress?: (feature: PoleFeature) => void;
  clock?: ClockStore;
}) {
  const directions = feature.pole.directions;
  const styles = useStyles();
  const { scheme } = useTheme();
  const cells = useNow((now) => nextCells(directions, now), clock);
  const { rows, more } = useMemo(() => markerRows(directions, cells), [directions, cells]);
  const tracks = useTracksViewChanges(directions, cells, stale, scheme);

  if (rows.length === 0) {
    return null;
  }
  return (
    <Marker
      coordinate={{ latitude: feature.pole.location.lat, longitude: feature.pole.location.lng }}
      anchor={{ x: 0.5, y: 1 }}
      tracksViewChanges={tracks}
      onPress={onPress ? () => onPress(feature) : undefined}
    >
      <View style={styles.bubble} accessibilityLabel={accessibilityText(feature, rows, stale)}>
        {rows.map((r) => (
          <Row key={directionKey(r.direction)} row={r} stale={stale} />
        ))}
        {more > 0 ? <Text style={styles.more}>+{more} more</Text> : null}
      </View>
    </Marker>
  );
}

function Row({ row: { direction, next }, stale }: { row: MarkerRow; stale: boolean }) {
  const styles = useStyles();
  return (
    <View style={styles.row}>
      <LineBadge direction={direction} />
      <Text style={styles.headsign} numberOfLines={1}>
        {direction.headsign}
      </Text>
      <DepartureTime cell={next} style={styles.countdown} stale={stale} />
    </View>
  );
}

function accessibilityText(feature: PoleFeature, rows: MarkerRow[], stale: boolean) {
  const parts = rows.map(
    (r) => `${r.direction.line} to ${r.direction.headsign}: ${cellDescription(r.next, stale)}`,
  );
  return [feature.station.name, ...parts].join('. ');
}

/**
 * tracksViewChanges for a custom marker (Google Maps; Apple Maps ignores
 * it): on after the content changed, off again shortly after, so the map
 * does not redraw the marker every frame (ARCHITECTURE §4.5). A switch
 * between light and dark mode is a content change too (task 7.1).
 */
function useTracksViewChanges(
  directions: Direction[],
  cells: (string | null)[],
  stale: boolean,
  scheme: ColorScheme,
): boolean {
  const content = useMemo(
    () => ({ directions, cells, stale, scheme }),
    [directions, cells, stale, scheme],
  );
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

const useStyles = themedStyles(({ scheme, colors, fontSize, fontWeight, radius, space }) => ({
  bubble: {
    minWidth: 120,
    maxWidth: 220,
    paddingVertical: space.xs,
    paddingHorizontal: space.sm,
    borderRadius: radius.md,
    backgroundColor: colors.surface,
    gap: 3, // optical: four rows stay compact
    // A shadow does not show on a dark map: an edge does.
    borderWidth: scheme === 'dark' ? StyleSheet.hairlineWidth : 0,
    borderColor: colors.border,
    shadowColor: colors.shadow,
    shadowOpacity: 0.2,
    shadowRadius: 3,
    shadowOffset: { width: 0, height: 1 },
    elevation: 3,
  },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.sm,
  },
  headsign: {
    flexShrink: 1,
    fontSize: fontSize.caption,
    color: colors.text,
  },
  countdown: {
    marginLeft: 'auto',
    fontSize: fontSize.caption,
    fontWeight: fontWeight.semibold,
    fontVariant: ['tabular-nums'],
    color: colors.text,
  },
  more: {
    fontSize: fontSize.micro,
    color: colors.textSecondary,
  },
}));
