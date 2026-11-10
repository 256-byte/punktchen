import {
  type ImageStyle,
  StyleSheet,
  type TextStyle,
  useColorScheme,
  type ViewStyle,
} from 'react-native';

import { type ColorScheme, type Theme, themes } from './tokens';

/**
 * The theme of the system's appearance (task 7.1): dark when the phone is
 * in dark mode, else light. Follows a switch while the app runs. No in-app
 * setting: the app looks like the rest of the phone.
 */
export function useTheme(): Theme {
  return themes[schemeOf(useColorScheme())];
}

/** 'dark' only when the system says so ('light', 'unspecified', null → light). */
export function schemeOf(system: string | null | undefined): ColorScheme {
  return system === 'dark' ? 'dark' : 'light';
}

/**
 * Styles that depend on the theme, created once per mode at module load:
 *
 *   const useStyles = themedStyles((t) => ({ title: { color: t.colors.text } }));
 *   function Title() { const styles = useStyles(); … }
 *
 * The hook returns the same objects on every render of a mode, so it does
 * not defeat memoisation, and nothing is created while rendering.
 */
export function themedStyles<T extends NamedStyles>(make: (theme: Theme) => T): () => T {
  const byScheme: Record<ColorScheme, T> = {
    light: StyleSheet.create(make(themes.light)),
    dark: StyleSheet.create(make(themes.dark)),
  };
  return function useThemedStyles() {
    return byScheme[useTheme().scheme];
  };
}

/**
 * What StyleSheet.create takes. As the constraint of `make`'s result it
 * keeps literal values typed ('row', not string).
 */
type NamedStyles = { readonly [name: string]: ViewStyle | TextStyle | ImageStyle };
