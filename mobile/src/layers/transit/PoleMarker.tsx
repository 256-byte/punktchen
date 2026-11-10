import { useEffect, useMemo, useState } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { Marker } from 'react-native-maps';

import { compactLines, type DetailLevel } from '@/core/clustering';
import {
  type Direction,
  directionHeadsign,
  directionKey,
  type MarkerRow,
  markerRows,
  nextCells,
  nonTerminalDirections,
  ringSymbol,
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

const MARKER_A11Y_HINT = 'Opens station departures';

/**
 * A pole or station cluster marker:
 * - At `poles` and `stations` zoom: one row per direction with an upcoming
 *   departure — line badge, headsign, countdown (task 3.5) coloured live or
 *   timetable, with its delay (4.5). Multi-pole station clusters show the
 *   station name at the top (7.2).
 * - At `compact` (overview) zoom: station name + line badges without
 *   headsigns or countdowns (7.2, ARCHITECTURE §4.5).
 * Directions with nothing coming are left out (station sheet, 3.6); a marker
 * with none draws nothing. Re-renders when a cell changes (about once a
 * minute), not on every tick. With stale data live countdowns are greyed out
 * (6.2). Colours follow light/dark mode (7.1).
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
  const directions = useMemo(
    () => nonTerminalDirections(feature.pole.directions, feature.station),
    [feature.pole.directions, feature.station],
  );
  const detail = feature.detail ?? 'poles';
  const poleCount = feature.poleCount ?? 1;
  const stationCount = feature.stationCount ?? 1;
  const styles = useStyles();
  const { scheme } = useTheme();
  const cells = useNow((now) => nextCells(directions, now), clock);
  const { rows, more } = useMemo(() => markerRows(directions, cells), [directions, cells]);
  const compact = useMemo(
    () => (detail === 'compact' ? compactLines(directions, cells) : null),
    [detail, directions, cells],
  );
  const tracks = useTracksViewChanges(directions, cells, stale, scheme, detail, stationCount);

  if (detail === 'compact') {
    if (!compact || compact.lines.length === 0) {
      return null;
    }
    const label = compactAccessibilityText(feature, compact.lines, compact.more);
    return (
      <Marker
        coordinate={{ latitude: feature.pole.location.lat, longitude: feature.pole.location.lng }}
        anchor={{ x: 0.5, y: 1 }}
        tracksViewChanges={tracks}
        accessibilityLabel={label}
        accessibilityHint={MARKER_A11Y_HINT}
        onPress={onPress ? () => onPress(feature) : undefined}
      >
        <View
          accessible
          accessibilityRole="button"
          accessibilityLabel={label}
          accessibilityHint={MARKER_A11Y_HINT}
          style={styles.compactBubble}
        >
          <View style={styles.header}>
            <Text style={styles.stationName} numberOfLines={1}>
              {feature.station.name}
            </Text>
            {stationCount > 1 ? (
              <Text style={styles.clusterCount}>{`+${stationCount - 1}`}</Text>
            ) : null}
          </View>
          <View style={styles.badges}>
            {compact.lines.map((d) => (
              <LineBadge key={`${d.mode}|${d.line}`} direction={d} />
            ))}
            {compact.more > 0 ? <Text style={styles.more}>{`+${compact.more}`}</Text> : null}
          </View>
        </View>
      </Marker>
    );
  }

  if (rows.length === 0) {
    return null;
  }
  const showStationHeader = detail === 'stations' && (poleCount > 1 || stationCount > 1);
  const label = accessibilityText(feature, rows, more, stale);
  return (
    <Marker
      coordinate={{ latitude: feature.pole.location.lat, longitude: feature.pole.location.lng }}
      anchor={{ x: 0.5, y: 1 }}
      tracksViewChanges={tracks}
      accessibilityLabel={label}
      accessibilityHint={MARKER_A11Y_HINT}
      onPress={onPress ? () => onPress(feature) : undefined}
    >
      <View
        accessible
        accessibilityRole="button"
        accessibilityLabel={label}
        accessibilityHint={MARKER_A11Y_HINT}
        style={styles.bubble}
      >
        {showStationHeader ? (
          <View style={styles.header}>
            <Text style={styles.stationName} numberOfLines={1}>
              {feature.station.name}
            </Text>
            {stationCount > 1 ? (
              <Text style={styles.clusterCount}>{`+${stationCount - 1}`}</Text>
            ) : null}
          </View>
        ) : null}
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
  const ring = ringSymbol(direction);
  return (
    <View style={styles.row}>
      <LineBadge direction={direction} />
      <Text style={styles.headsign} numberOfLines={1}>
        {ring ? (
          <>
            <Text style={styles.ringSymbol}>{`${ring} `}</Text>
            {direction.headsign}
          </>
        ) : (
          direction.headsign
        )}
      </Text>
      <DepartureTime cell={next} style={styles.countdown} stale={stale} />
    </View>
  );
}

function accessibilityText(feature: PoleFeature, rows: MarkerRow[], more: number, stale: boolean) {
  const stationCount = feature.stationCount ?? 1;
  const header =
    stationCount > 1
      ? `${feature.station.name} and ${stationCount - 1} nearby stops`
      : feature.station.name;
  const parts = rows.map(
    (r) =>
      `${r.direction.line} to ${directionHeadsign(r.direction)}: ${cellDescription(r.next, stale)}`,
  );
  if (more > 0) {
    parts.push(`${more} more ${more === 1 ? 'direction' : 'directions'}`);
  }
  return [header, ...parts].join('. ');
}

function compactAccessibilityText(feature: PoleFeature, lines: readonly Direction[], more: number) {
  const stationCount = feature.stationCount ?? 1;
  const header =
    stationCount > 1
      ? `${feature.station.name} and ${stationCount - 1} nearby stops`
      : feature.station.name;
  const names = lines.map((d) => d.line).join(', ');
  const tail = more > 0 ? `, and ${more} more` : '';
  return `${header}. Lines: ${names}${tail}`;
}

/**
 * tracksViewChanges for a custom marker (Google Maps; Apple Maps ignores
 * it): on after the content changed, off again shortly after, so the map
 * does not redraw the marker every frame (ARCHITECTURE §4.5). A switch
 * between light and dark mode or zoom detail is a content change too.
 */
function useTracksViewChanges(
  directions: Direction[],
  cells: (string | null)[],
  stale: boolean,
  scheme: ColorScheme,
  detail: DetailLevel,
  stationCount: number,
): boolean {
  const content = useMemo(
    () => ({ directions, cells, stale, scheme, detail, stationCount }),
    [directions, cells, stale, scheme, detail, stationCount],
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
  compactBubble: {
    maxWidth: 180,
    paddingVertical: space.xs,
    paddingHorizontal: space.sm,
    borderRadius: radius.md,
    backgroundColor: colors.surface,
    gap: space.xxs,
    borderWidth: scheme === 'dark' ? StyleSheet.hairlineWidth : 0,
    borderColor: colors.border,
    shadowColor: colors.shadow,
    shadowOpacity: 0.2,
    shadowRadius: 3,
    shadowOffset: { width: 0, height: 1 },
    elevation: 3,
  },
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.xs,
  },
  stationName: {
    flexShrink: 1,
    fontSize: fontSize.micro,
    fontWeight: fontWeight.bold,
    color: colors.text,
  },
  clusterCount: {
    fontSize: fontSize.micro,
    fontWeight: fontWeight.semibold,
    color: colors.textSecondary,
  },
  badges: {
    flexDirection: 'row',
    alignItems: 'center',
    flexWrap: 'wrap',
    gap: space.xs,
  },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.sm,
  },
  headsign: {
    flexShrink: 1,
    fontSize: fontSize.caption,
    lineHeight: fontSize.bodyLarge,
    color: colors.text,
  },
  ringSymbol: {
    // U+27F2/U+27F3 glyphs sit at ~65 % of the em box in the system symbol font;
    // scaling up brings ⟳/⟲ to the cap height of the caption text.
    fontSize: fontSize.headline,
    lineHeight: fontSize.bodyLarge,
    fontWeight: fontWeight.bold,
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
