// Design tokens (task 7.1): colours for light and dark mode, type scale,
// spacing, radii. Plain data: components read them through useTheme() /
// themedStyles() (./theme.ts). Line badge colours are not here: they are
// the lines' own (API `color`, else MODE_COLORS in src/core/directions.ts)
// and the same in both modes.
//
// Text colours meet WCAG AA (4.5:1) on `background` and `surface` of their
// mode; tests/ui/theme.test.tsx checks every pair.

export type ColorScheme = 'light' | 'dark';

export interface Colors {
  /** Full-screen backgrounds (location screens, behind the map). */
  background: string;
  /** Cards on the map (marker bubbles, menus) and the bottom sheet. */
  surface: string;
  /** Translucent `surface` for boxes floating over the map (banner, error box). */
  overlay: string;
  /** More translucent `surface` for the map's attribution line. */
  overlaySubtle: string;
  /** Main text; also timetable-only departure times. */
  text: string;
  /** Notes, details, platform numbers. */
  textSecondary: string;
  /** Section titles, legends, "+N more". */
  textTertiary: string;
  /** Hairline borders and separators. */
  border: string;
  /** Bottom sheet drag handle. */
  handle: string;
  /** Filled buttons. */
  accent: string;
  /** Text on `accent`. */
  onAccent: string;
  /** Text buttons and links on `surface` / `background`. */
  accentText: string;
  /** Error messages. */
  danger: string;
  /** "All good" messages (backend status). */
  success: string;
  /** Live departure on time or early (task 4.5). */
  live: string;
  /** Live departure late, and cancelled ones (task 4.5). */
  late: string;
  /** Live values of data older than 5 min (task 6.2): last known, not current. */
  stale: string;
  /** Status banner after 5 min without new data (task 6.2). */
  warningSurface: string;
  warningBorder: string;
  warningText: string;
  /** Developer tools' chip: the opposite of the current mode. */
  inverseSurface: string;
  onInverse: string;
  /** Drop shadows (iOS; Android uses elevation). */
  shadow: string;
}

const light: Colors = {
  background: '#FFFFFF',
  surface: '#FFFFFF',
  overlay: 'rgba(255, 255, 255, 0.95)',
  overlaySubtle: 'rgba(255, 255, 255, 0.8)',
  text: '#111111',
  textSecondary: '#555555',
  textTertiary: '#666666',
  border: '#C8C8CC',
  handle: '#B0B0B5',
  accent: '#1F6FEB',
  onAccent: '#FFFFFF',
  accentText: '#1A61D1',
  danger: '#B42318',
  success: '#067647',
  live: '#067647',
  late: '#B42318',
  stale: '#767676',
  warningSurface: '#FFFAEB',
  warningBorder: '#B54708',
  warningText: '#93370D',
  inverseSurface: '#2C2C2E',
  onInverse: '#FFFFFF',
  shadow: '#000000',
};

const dark: Colors = {
  background: '#000000',
  surface: '#1C1C1E',
  overlay: 'rgba(28, 28, 30, 0.95)',
  overlaySubtle: 'rgba(28, 28, 30, 0.8)',
  text: '#F2F2F7',
  textSecondary: '#AEAEB2',
  textTertiary: '#98989F',
  border: '#48484A',
  handle: '#636366',
  accent: '#1F6FEB',
  onAccent: '#FFFFFF',
  accentText: '#6EA8FE',
  danger: '#FF7B72',
  success: '#3FB950',
  live: '#3FB950',
  late: '#FF7B72',
  stale: '#8E8E93',
  warningSurface: '#2E1F05',
  warningBorder: '#F79009',
  warningText: '#FEC84B',
  inverseSurface: '#E5E5EA',
  onInverse: '#111111',
  shadow: '#000000',
};

/** Font weights in use. */
export const fontWeight = {
  regular: '400',
  semibold: '600',
  bold: '700',
} as const;

/**
 * Type scale (font sizes in pt). Sizes are kept from Phases 3–6; names say
 * where they belong.
 */
export const fontSize = {
  /** Attribution, "+N more" on markers. */
  micro: 11,
  /** Marker rows, legends, banner details. */
  caption: 12,
  /** Banner titles, section titles, small buttons, platforms. */
  footnote: 13,
  /** Notes, departure times in the sheet. */
  callout: 14,
  /** Rows in the sheet and in menus. */
  body: 15,
  /** Text and buttons on full screens. */
  bodyLarge: 16,
  /** Headings of status boxes. */
  headline: 18,
  /** Station name in the sheet. */
  title: 20,
  /** Headings of full screens. */
  titleLarge: 22,
} as const;

/** Spacing scale (dp): paddings, margins, gaps. */
export const space = {
  xxs: 2,
  xs: 4,
  sm: 6,
  md: 8,
  ml: 10,
  lg: 12,
  xl: 16,
  xxl: 24,
  xxxl: 32,
} as const;

/** Corner radii (dp). */
export const radius = {
  /** Small line badges, attribution. */
  xs: 4,
  /** Large line badges. */
  sm: 6,
  /** Marker bubbles, small buttons. */
  md: 8,
  /** Banner, menus, large buttons. */
  lg: 10,
  /** Status boxes. */
  xl: 12,
} as const;

/** Opacities. */
export const opacity = {
  /** A pressed or busy button. */
  pressed: 0.6,
  /** Directions with nothing coming in the sheet. */
  dimmed: 0.5,
} as const;

export interface Theme {
  scheme: ColorScheme;
  colors: Colors;
  fontSize: typeof fontSize;
  fontWeight: typeof fontWeight;
  space: typeof space;
  radius: typeof radius;
  opacity: typeof opacity;
}

const shared = { fontSize, fontWeight, space, radius, opacity };

/** Both themes, built once (themes[scheme] is a stable object). */
export const themes: Record<ColorScheme, Theme> = {
  light: { scheme: 'light', colors: light, ...shared },
  dark: { scheme: 'dark', colors: dark, ...shared },
};
