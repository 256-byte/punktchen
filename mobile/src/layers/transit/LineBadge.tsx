import { StyleSheet, Text, View } from 'react-native';

import { badgeColor, type Direction, textColorOn } from '@/core/directions';

/** The line name on its colour (marker rows and the station sheet). */
export function LineBadge({
  direction,
  size = 'small',
}: {
  direction: Pick<Direction, 'line' | 'color' | 'mode'>;
  size?: 'small' | 'large';
}) {
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

const styles = StyleSheet.create({
  badge: {
    minWidth: 30,
    paddingHorizontal: 4,
    paddingVertical: 1,
    borderRadius: 4,
    alignItems: 'center',
  },
  large: {
    minWidth: 44,
    paddingHorizontal: 6,
    paddingVertical: 3,
    borderRadius: 6,
  },
  line: {
    fontSize: 12,
    fontWeight: '700',
  },
  lineLarge: {
    fontSize: 15,
  },
});
