// Colour maths for the design tokens (task 7.1) and line badges (3.5):
// WCAG 2.x relative luminance and contrast ratio of #RRGGBB colours.

const HEX = /^#([0-9a-f]{6})$/i;

/** Relative luminance (0 black … 1 white) of "#RRGGBB"; null for anything else. */
export function relativeLuminance(hex: string): number | null {
  const m = HEX.exec(hex);
  if (!m) {
    return null;
  }
  const n = parseInt(m[1]!, 16);
  const channel = (shift: number) => {
    const c = ((n >> shift) & 0xff) / 255;
    return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * channel(16) + 0.7152 * channel(8) + 0.0722 * channel(0);
}

/**
 * Contrast ratio of two "#RRGGBB" colours, 1 … 21 (order does not matter).
 * WCAG AA: 4.5 for text, 3 for large or bold text and for UI parts.
 * Throws for colours it cannot read: callers check fixed token pairs.
 */
export function contrastRatio(a: string, b: string): number {
  const la = relativeLuminance(a);
  const lb = relativeLuminance(b);
  if (la === null || lb === null) {
    throw new Error(`contrastRatio: not a #RRGGBB colour: ${la === null ? a : b}`);
  }
  const [hi, lo] = la >= lb ? [la, lb] : [lb, la];
  return (hi + 0.05) / (lo + 0.05);
}
