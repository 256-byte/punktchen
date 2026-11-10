// Small mode icons next to a stop's name (task 7.10). Our own drawings, no
// operator logos (user decision 2026-11-10): S-Bahn is a white "S" on a
// green circle and U-Bahn a white "U" on a blue square, as on Berlin's
// signs; tram, bus, regional train and ferry are white pictograms on a
// rounded square in the mode's colour.
//
// Pictogram paths: Material Icons "tram", "directions_bus", "train" and
// "directions_boat" by Google (https://github.com/google/material-design-icons),
// Apache License 2.0 (https://www.apache.org/licenses/LICENSE-2.0).
import { StyleSheet, Text, View } from 'react-native';
import Svg, { Path } from 'react-native-svg';

import { type Mode, MODE_COLORS } from '@/core/directions';
import { fontWeight } from '@/ui/tokens';

/** Edge length of an icon in points: the height of the marker's station name. */
export const MODE_ICON_SIZE = 14;

const PICTOGRAMS: Partial<Record<Mode, string>> = {
  tram: 'M19 16.94V8.5c0-2.79-2.61-3.4-6.01-3.49l.76-1.51H17V2H7v1.5h4.75l-.76 1.52C7.86 5.11 5 5.73 5 8.5v8.44c0 1.45 1.19 2.66 2.59 2.97L6 21.5v.5h2.23l2-2H14l2 2h2v-.5L16.5 20h-.08c1.69 0 2.58-1.37 2.58-3.06zm-7 1.56c-.83 0-1.5-.67-1.5-1.5s.67-1.5 1.5-1.5 1.5.67 1.5 1.5-.67 1.5-1.5 1.5zm5-4.5H7V9h10v5z',
  bus: 'M4 16c0 .88.39 1.67 1 2.22V20c0 .55.45 1 1 1h1c.55 0 1-.45 1-1v-1h8v1c0 .55.45 1 1 1h1c.55 0 1-.45 1-1v-1.78c.61-.55 1-1.34 1-2.22V6c0-3.5-3.58-4-8-4s-8 .5-8 4v10zm3.5 1c-.83 0-1.5-.67-1.5-1.5S6.67 14 7.5 14s1.5.67 1.5 1.5S8.33 17 7.5 17zm9 0c-.83 0-1.5-.67-1.5-1.5s.67-1.5 1.5-1.5 1.5.67 1.5 1.5-.67 1.5-1.5 1.5zm1.5-6H6V6h12v5z',
  regional:
    'M12 2c-4 0-8 .5-8 4v9.5C4 17.43 5.57 19 7.5 19L6 20.5v.5h2.23l2-2H14l2 2h2v-.5L16.5 19c1.93 0 3.5-1.57 3.5-3.5V6c0-3.5-3.58-4-8-4zM7.5 17c-.83 0-1.5-.67-1.5-1.5S6.67 14 7.5 14s1.5.67 1.5 1.5S8.33 17 7.5 17zm3.5-7H6V6h5v4zm2 0V6h5v4h-5zm3.5 7c-.83 0-1.5-.67-1.5-1.5s.67-1.5 1.5-1.5 1.5.67 1.5 1.5-.67 1.5-1.5 1.5z',
  ferry:
    'M20 21c-1.39 0-2.78-.47-4-1.32-2.44 1.71-5.56 1.71-8 0C6.78 20.53 5.39 21 4 21H2v2h2c1.38 0 2.74-.35 4-.99 2.52 1.29 5.48 1.29 8 0 1.26.65 2.62.99 4 .99h2v-2h-2zM3.95 19H4c1.6 0 3.02-.88 4-2 .98 1.12 2.4 2 4 2s3.02-.88 4-2c.98 1.12 2.4 2 4 2h.05l1.89-6.68c.08-.26.06-.54-.06-.78s-.34-.42-.6-.5L20 10.62V6c0-1.1-.9-2-2-2h-3V1H9v3H6c-1.1 0-2 .9-2 2v4.62l-1.29.42c-.26.08-.48.26-.6.5s-.15.52-.06.78L3.95 19zM6 6h12v3.97L12 8 6 9.97V6z',
};

/**
 * The icon of a mode; nothing for `other`. Decorative: the marker's
 * accessibility label names the modes.
 */
export function ModeIcon({ mode, size = MODE_ICON_SIZE }: { mode: Mode; size?: number }) {
  const background = MODE_COLORS[mode];
  if (mode === 'sbahn' || mode === 'ubahn') {
    return (
      <View
        testID={`mode-icon-${mode}`}
        style={[
          styles.letterBox,
          {
            width: size,
            height: size,
            backgroundColor: background,
            borderRadius: mode === 'sbahn' ? size / 2 : size * 0.15,
          },
        ]}
      >
        <Text
          allowFontScaling={false}
          style={[styles.letter, { fontSize: size * 0.78, lineHeight: size }]}
        >
          {mode === 'sbahn' ? 'S' : 'U'}
        </Text>
      </View>
    );
  }
  const path = PICTOGRAMS[mode];
  if (!path) return null;
  return (
    <View
      testID={`mode-icon-${mode}`}
      style={[
        styles.letterBox,
        { width: size, height: size, backgroundColor: background, borderRadius: size * 0.2 },
      ]}
    >
      <Svg width={size * 0.8} height={size * 0.8} viewBox="0 0 24 24">
        <Path d={path} fill="#FFFFFF" />
      </Svg>
    </View>
  );
}

const styles = StyleSheet.create({
  letterBox: {
    alignItems: 'center',
    justifyContent: 'center',
    overflow: 'hidden',
  },
  letter: {
    color: '#FFFFFF',
    fontWeight: fontWeight.bold,
    textAlign: 'center',
    includeFontPadding: false,
  },
});
