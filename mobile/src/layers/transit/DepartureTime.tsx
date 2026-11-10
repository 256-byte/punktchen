import { type StyleProp, StyleSheet, Text, type TextStyle } from 'react-native';

import { cellDescription, type DepartureCell, type DepartureState, isLive } from '@/core/realtime';

/** Live times on time or early: green; late: red (like the usual journey planners). */
export const LIVE_GREEN = '#067647';
export const LATE_RED = '#b42318';
/** Live values of data older than 5 min: last known, not current. */
export const STALE_GREY = '#8a8a8a';

/**
 * A departure's countdown (task 4.5): timetable times in the caller's
 * colour; live ones green, or red with their delay ("3 min +2") when late;
 * early ones green with "−1"; cancelled ones struck through. With stale
 * data (task 6.2) live ones turn grey, so the timetable times stand out.
 */
export function DepartureTime({
  cell,
  style,
  stale = false,
}: {
  cell: DepartureCell;
  style?: StyleProp<TextStyle>;
  stale?: boolean;
}) {
  const outdated = stale && isLive(cell.state);
  return (
    <Text
      style={[style, STATE_STYLES[cell.state], outdated && styles.outdated]}
      accessibilityLabel={cellDescription(cell, stale)}
    >
      {cell.label}
      {cell.delay ? <Text style={styles.delay}>{` ${cell.delay}`}</Text> : null}
    </Text>
  );
}

const STATE_STYLES: Record<DepartureState, TextStyle> = StyleSheet.create({
  scheduled: {},
  onTime: { color: LIVE_GREEN },
  early: { color: LIVE_GREEN },
  late: { color: LATE_RED },
  cancelled: { color: LATE_RED, textDecorationLine: 'line-through' },
});

const styles = StyleSheet.create({
  delay: { fontWeight: '700' },
  outdated: { color: STALE_GREY },
});
