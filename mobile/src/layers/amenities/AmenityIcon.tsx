// Icons of the amenity layers (Phase 9), our own drawings like the S and U
// signs (task 7.10): "WC" on a teal circle for toilets, a water drop on a
// blue circle for drinking fountains.
import { StyleSheet, Text, View } from 'react-native';
import Svg, { Path } from 'react-native-svg';

import { fontWeight } from '@/ui/tokens';

export type AmenityKind = 'toilet' | 'fountain';

/** The same in light and dark mode, like the line colours. */
export const AMENITY_COLORS: Record<AmenityKind, string> = {
  toilet: '#00796B',
  fountain: '#1565C0',
};

/** A drop: a point on top, round at the bottom (24 × 24 view box). */
const DROP = 'M12 2.5C12 2.5 5.5 10.2 5.5 14.6a6.5 6.5 0 0 0 13 0C18.5 10.2 12 2.5 12 2.5z';

/** Edge length of the marker icon in points. */
export const AMENITY_ICON_SIZE = 26;

/** Decorative: markers and sheets carry their own accessibility labels. */
export function AmenityIcon({
  kind,
  size = AMENITY_ICON_SIZE,
}: {
  kind: AmenityKind;
  size?: number;
}) {
  return (
    <View
      testID={`amenity-icon-${kind}`}
      style={[
        styles.circle,
        {
          width: size,
          height: size,
          borderRadius: size / 2,
          backgroundColor: AMENITY_COLORS[kind],
        },
      ]}
    >
      {kind === 'toilet' ? (
        <Text allowFontScaling={false} style={[styles.wc, { fontSize: size * 0.4 }]}>
          WC
        </Text>
      ) : (
        <Svg width={size * 0.7} height={size * 0.7} viewBox="0 0 24 24">
          <Path d={DROP} fill="#FFFFFF" />
        </Svg>
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  circle: {
    alignItems: 'center',
    justifyContent: 'center',
    borderWidth: 2,
    borderColor: '#FFFFFF',
  },
  wc: {
    color: '#FFFFFF',
    fontWeight: fontWeight.bold,
    includeFontPadding: false,
  },
});
