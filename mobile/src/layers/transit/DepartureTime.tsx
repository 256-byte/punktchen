import { type StyleProp, Text, type TextStyle } from 'react-native';

import { cellDescription, type DepartureCell, type DepartureState, isLive } from '@/core/realtime';
import { themedStyles } from '@/ui/theme';

/**
 * A departure's countdown (task 4.5): timetable times in the caller's
 * colour; live ones green, or red with their delay ("3 min +2") when late;
 * early ones green with "−1"; cancelled ones struck through. With stale
 * data (task 6.2) live ones turn grey, so the timetable times stand out.
 * Colours: the theme's `live`, `late` and `stale` (task 7.1).
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
  const styles = useStyles();
  const outdated = stale && isLive(cell.state);
  return (
    <Text
      style={[style, styles[cell.state], outdated && styles.outdated]}
      accessibilityLabel={cellDescription(cell, stale)}
    >
      {cell.label}
      {cell.delay ? <Text style={styles.delay}>{` ${cell.delay}`}</Text> : null}
    </Text>
  );
}

const useStyles = themedStyles(
  ({ colors, fontWeight }) =>
    ({
      scheduled: {},
      onTime: { color: colors.live },
      early: { color: colors.live },
      late: { color: colors.late },
      cancelled: { color: colors.late, textDecorationLine: 'line-through' },
      delay: { fontWeight: fontWeight.bold },
      outdated: { color: colors.stale },
    }) satisfies Record<DepartureState | 'delay' | 'outdated', TextStyle>,
);
