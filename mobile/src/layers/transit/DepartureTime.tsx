import { type StyleProp, StyleSheet, Text, type TextStyle } from 'react-native';

import { cellDescription, type DepartureCell, type DepartureState } from '@/core/realtime';

/** Live times on time or early: green; late: red (like the usual journey planners). */
export const LIVE_GREEN = '#067647';
export const LATE_RED = '#b42318';

/**
 * A departure's countdown (task 4.5): timetable times in the caller's
 * colour; live ones green, or red with their delay ("3 min +2") when late;
 * early ones green with "−1"; cancelled ones struck through.
 */
export function DepartureTime({
  cell,
  style,
}: {
  cell: DepartureCell;
  style?: StyleProp<TextStyle>;
}) {
  return (
    <Text style={[style, STATE_STYLES[cell.state]]} accessibilityLabel={cellDescription(cell)}>
      {cell.label}
      {cell.delay ? <Text style={styles.delay}>{` ${cell.delay}`}</Text> : null}
    </Text>
  );
}

const styles = StyleSheet.create({
  scheduled: {},
  onTime: { color: LIVE_GREEN },
  early: { color: LIVE_GREEN },
  late: { color: LATE_RED },
  cancelled: { color: LATE_RED, textDecorationLine: 'line-through' },
  delay: { fontWeight: '700' },
});

const STATE_STYLES: Record<DepartureState, TextStyle> = styles;
