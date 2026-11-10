import { Text, View } from 'react-native';

import { badgeColor, type Direction, textColorOn } from '@/core/directions';
import { themedStyles } from '@/ui/theme';

/**
 * The line name on its colour (marker rows and the station sheet). The
 * line colours are the same in light and dark mode (task 7.1).
 */
export function LineBadge({
  direction,
  size = 'small',
}: {
  direction: Pick<Direction, 'line' | 'color' | 'mode'>;
  size?: 'small' | 'large';
}) {
  const styles = useStyles();
  const background = badgeColor(direction);
  return (
    <View style={[styles.badge, size === 'large' && styles.large, { backgroundColor: background }]}>
      <Text
        style={[
          styles.line,
          size === 'large' && styles.lineLarge,
          { color: textColorOn(background) },
        ]}
      >
        {direction.line}
      </Text>
    </View>
  );
}

const useStyles = themedStyles(({ fontSize, fontWeight, radius, space }) => ({
  badge: {
    minWidth: 30,
    paddingHorizontal: space.xs,
    paddingVertical: 1, // optical: keeps the marker rows compact
    borderRadius: radius.xs,
    alignItems: 'center',
  },
  large: {
    minWidth: 44,
    paddingHorizontal: space.sm,
    paddingVertical: space.xxs,
    borderRadius: radius.sm,
  },
  line: {
    fontSize: fontSize.caption,
    fontWeight: fontWeight.bold,
  },
  lineLarge: {
    fontSize: fontSize.body,
  },
}));
