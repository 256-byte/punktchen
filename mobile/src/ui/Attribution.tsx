import { Text, View, type ViewStyle } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { themedStyles } from './theme';

/**
 * Data attribution (task 3.7, ARCHITECTURE §6): the response's
 * meta.attribution, e.g. "Data: VBB (CC BY 4.0)". Shown wherever its data
 * is shown; nothing when there is nothing to attribute.
 */
export function Attribution({ lines, style }: { lines: readonly string[]; style?: ViewStyle }) {
  const styles = useStyles();
  if (lines.length === 0) {
    return null;
  }
  return (
    <View style={[style, styles.passThrough]}>
      <Text style={styles.text}>{lines.join(' · ')}</Text>
    </View>
  );
}

/** On the map: bottom right (Apple Maps keeps its "Legal" link bottom left). */
export function MapAttribution({ lines }: { lines: readonly string[] }) {
  const insets = useSafeAreaInsets();
  const styles = useStyles();
  return (
    <Attribution
      lines={lines}
      style={{ ...styles.map, bottom: Math.max(insets.bottom, 8), right: insets.right + 8 }}
    />
  );
}

const useStyles = themedStyles(({ colors, fontSize, radius, space }) => ({
  map: {
    position: 'absolute',
    paddingHorizontal: space.sm,
    paddingVertical: space.xxs,
    borderRadius: radius.xs,
    backgroundColor: colors.overlaySubtle,
  },
  passThrough: {
    pointerEvents: 'none',
  },
  text: {
    fontSize: fontSize.micro,
    color: colors.textSecondary,
  },
}));
