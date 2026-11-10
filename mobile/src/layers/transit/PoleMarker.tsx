import { useEffect, useMemo, useState } from 'react';
import { type LayoutChangeEvent, StyleSheet, Text, View } from 'react-native';
import { Marker } from 'react-native-maps';

import { compactLines, type DetailLevel } from '@/core/clustering';
import type { LatLng } from '@/core/geo';
import type { Size } from '@/core/labelLayout';
import {
  type Direction,
  directionHeadsign,
  directionKey,
  type MarkerRow,
  markerModes,
  markerRows,
  type Mode,
  MODE_NAMES,
  nextCells,
  nonTerminalDirections,
  ringSymbol,
  sectionRows,
} from '@/core/directions';
import { cellDescription } from '@/core/realtime';
import { type ClockStore, clockStore, useNow } from '@/state/clock';
import { themedStyles, useTheme } from '@/ui/theme';
import type { ColorScheme } from '@/ui/tokens';

import { DepartureTime } from './DepartureTime';
import type { PoleFeature } from './index';
import { LineBadge } from './LineBadge';
import { ModeIcon } from './ModeIcon';

/** How long a marker redraws its native snapshot after its content changed. */
export const TRACK_CHANGES_MS = 500;

const MARKER_A11Y_HINT = 'Opens station departures';

/**
 * A pole or station cluster marker. At every zoom its top line shows the
 * icons of its modes and the station's name (task 7.10).
 * - At `poles` and `stations` zoom: one row per direction with an upcoming
 *   departure — line badge, headsign, countdown (task 3.5) coloured live or
 *   timetable, with its delay (4.5).
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
  displayAt,
  onSize,
}: {
  feature: PoleFeature;
  /** The data is older than 5 min (ARCHITECTURE §4.3). */
  stale?: boolean;
  /** Tap: open the station sheet (task 3.6). */
  onPress?: (feature: PoleFeature) => void;
  /** Draw here instead of at the pole: moved aside by the map (task 7.9). */
  displayAt?: LatLng;
  /** Reports the bubble's size, 0 × 0 when nothing is drawn (task 7.9). */
  onSize?: (size: Size) => void;
  clock?: ClockStore;
}) {
  const detail = feature.detail ?? 'poles';
  // Combined stop (task 7.8): each pole is a section; otherwise one section.
  const sectionDirections = useMemo(() => {
    const poles = detail === 'poles' && feature.sections ? feature.sections : [feature.pole];
    return poles.map((p) => nonTerminalDirections(p.directions, feature.station));
  }, [detail, feature.sections, feature.pole, feature.station]);
  const directions = useMemo(() => sectionDirections.flat(), [sectionDirections]);
  const modes = useMemo(() => markerModes(directions), [directions]);
  const stationCount = feature.stationCount ?? 1;
  const styles = useStyles();
  const { scheme } = useTheme();
  const cells = useNow((now) => nextCells(directions, now), clock);
  const { sections, more } = useMemo(() => {
    if (sectionDirections.length > 1) return sectionRows(sectionDirections, cells);
    const single = markerRows(directions, cells);
    return { sections: single.rows.length > 0 ? [single.rows] : [], more: single.more };
  }, [sectionDirections, directions, cells]);
  const rows = useMemo(() => sections.flat(), [sections]);
  const compact = useMemo(
    () => (detail === 'compact' ? compactLines(directions, cells) : null),
    [detail, directions, cells],
  );
  const tracks = useTracksViewChanges(directions, cells, stale, scheme, detail, stationCount);

  const at = displayAt ?? feature.pole.location;
  const coordinate = useMemo(() => ({ latitude: at.lat, longitude: at.lng }), [at.lat, at.lng]);
  const drawsNothing =
    detail === 'compact' ? !compact || compact.lines.length === 0 : rows.length === 0;
  // Nothing drawn: take no room on the map (task 7.9).
  useEffect(() => {
    if (drawsNothing) onSize?.({ width: 0, height: 0 });
  }, [drawsNothing, onSize]);
  const reportLayout = (e: LayoutChangeEvent) => {
    const { width, height } = e.nativeEvent.layout;
    onSize?.({ width, height });
  };

  if (detail === 'compact') {
    if (!compact || compact.lines.length === 0) {
      return null;
    }
    const label = compactAccessibilityText(feature, modes, compact.lines, compact.more);
    return (
      <Marker
        coordinate={coordinate}
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
          onLayout={reportLayout}
        >
          <Header name={feature.station.name} modes={modes} stationCount={stationCount} />
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
  const label = accessibilityText(feature, modes, rows, more, stale);
  return (
    <Marker
      coordinate={coordinate}
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
        onLayout={reportLayout}
      >
        <Header name={feature.station.name} modes={modes} stationCount={stationCount} />
        {sections.map((section, s) => (
          <View
            key={s}
            style={s > 0 ? [styles.section, styles.divided] : styles.section}
            testID={`section-${s}`}
          >
            {section.map((r) => (
              <Row key={directionKey(r.direction)} row={r} stale={stale} />
            ))}
          </View>
        ))}
        {more > 0 ? <Text style={styles.more}>+{more} more</Text> : null}
      </View>
    </Marker>
  );
}

/**
 * Top line of every marker (task 7.10): the icons of the modes it stands for,
 * the station's name, and "+N" when nearby stops are merged in.
 */
function Header({
  name,
  modes,
  stationCount,
}: {
  name: string;
  modes: readonly Mode[];
  stationCount: number;
}) {
  const styles = useStyles();
  return (
    <View style={styles.header} testID="marker-header">
      {modes.length > 0 ? (
        <View style={styles.icons}>
          {modes.map((m) => (
            <ModeIcon key={m} mode={m} />
          ))}
        </View>
      ) : null}
      <Text style={styles.stationName} numberOfLines={1}>
        {name}
      </Text>
      {stationCount > 1 ? <Text style={styles.clusterCount}>{`+${stationCount - 1}`}</Text> : null}
    </View>
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

/** "S+U Wedding, S-Bahn, U-Bahn" or "S+U Alexanderplatz and 2 nearby stops, …". */
function headerText(feature: PoleFeature, modes: readonly Mode[]) {
  const stationCount = feature.stationCount ?? 1;
  const name =
    stationCount > 1
      ? `${feature.station.name} and ${stationCount - 1} nearby stops`
      : feature.station.name;
  return [name, ...modes.map((m) => MODE_NAMES[m])].join(', ');
}

function accessibilityText(
  feature: PoleFeature,
  modes: readonly Mode[],
  rows: MarkerRow[],
  more: number,
  stale: boolean,
) {
  const header = headerText(feature, modes);
  const parts = rows.map(
    (r) =>
      `${r.direction.line} to ${directionHeadsign(r.direction)}: ${cellDescription(r.next, stale)}`,
  );
  if (more > 0) {
    parts.push(`${more} more ${more === 1 ? 'direction' : 'directions'}`);
  }
  return [header, ...parts].join('. ');
}

function compactAccessibilityText(
  feature: PoleFeature,
  modes: readonly Mode[],
  lines: readonly Direction[],
  more: number,
) {
  const header = headerText(feature, modes);
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
  icons: {
    flexDirection: 'row',
    gap: 2,
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
  section: {
    gap: 3, // same row spacing as the bubble (every section, the first one too)
  },
  divided: {
    // Divider between the poles of a combined stop (task 7.8).
    borderTopWidth: StyleSheet.hairlineWidth,
    borderTopColor: colors.border,
    paddingTop: 3,
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
