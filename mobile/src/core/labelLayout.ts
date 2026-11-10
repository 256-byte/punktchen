// Overlap-free marker layout (task 7.9, ARCHITECTURE §4.5). Pure TypeScript:
// takes each marker's point on the screen and the size of its bubble, and
// returns how far to move each bubble (in screen points) so that no two
// bubbles overlap. Markers that do not touch anything stay where they are,
// so when the user zooms in far enough every bubble is back on its stop.

export interface ScreenPoint {
  x: number;
  y: number;
}

export interface Size {
  width: number;
  height: number;
}

/**
 * Where a bubble sits relative to its point, as fractions of its size
 * (react-native-maps `anchor`): { x: 0.5, y: 1 } = bottom centre on the point
 * (Google Maps), { x: 0.5, y: 0.5 } = centred (Apple Maps ignores `anchor`).
 */
export interface Anchor {
  x: number;
  y: number;
}

export interface LabelInput {
  key: string;
  /** The stop's position on the screen. */
  point: ScreenPoint;
  size: Size;
}

export interface LayoutOptions {
  anchor: Anchor;
  /** Space kept between two bubbles. */
  gap?: number;
  /** A bubble moves at most this far; beyond it, it stays and overlaps. */
  maxShift?: number;
  /** Distance between two tried positions on the way out. */
  step?: number;
}

/** Offset of a bubble from its point, in screen points; {0, 0}: in place. */
export interface Offset {
  dx: number;
  dy: number;
}

export const LAYOUT_GAP = 4;
export const LAYOUT_MAX_SHIFT = 260;
export const LAYOUT_STEP = 12;

/**
 * Directions tried at each distance, sideways first (the user asked for
 * bubbles "pushed to the side"), then up and down, then the diagonals.
 * Unit vectors; y grows downwards as on the screen.
 */
const DIRECTIONS: readonly ScreenPoint[] = [
  { x: 1, y: 0 },
  { x: -1, y: 0 },
  { x: 0, y: -1 },
  { x: 0, y: 1 },
  { x: Math.SQRT1_2, y: -Math.SQRT1_2 },
  { x: -Math.SQRT1_2, y: -Math.SQRT1_2 },
  { x: Math.SQRT1_2, y: Math.SQRT1_2 },
  { x: -Math.SQRT1_2, y: Math.SQRT1_2 },
];

interface Rect {
  left: number;
  top: number;
  right: number;
  bottom: number;
}

function rectAt(label: LabelInput, anchor: Anchor, offset: Offset, gap: number): Rect {
  const left = label.point.x + offset.dx - anchor.x * label.size.width;
  const top = label.point.y + offset.dy - anchor.y * label.size.height;
  const half = gap / 2;
  return {
    left: left - half,
    top: top - half,
    right: left + label.size.width + half,
    bottom: top + label.size.height + half,
  };
}

function overlaps(a: Rect, b: Rect): boolean {
  return a.left < b.right && b.left < a.right && a.top < b.bottom && b.top < a.bottom;
}

/**
 * Places the bubbles one by one in the given order (first = most important,
 * it never moves): each stays at its point if it overlaps none of the ones
 * placed before, else moves to the nearest free position — tried at growing
 * distances `step`, `2·step`, … up to `maxShift`, in DIRECTIONS order. A
 * bubble with no free position within `maxShift` stays at its point.
 *
 * Deterministic and independent of where the screen is: moving every point
 * by the same amount (a pan) gives the same offsets.
 */
export function layoutLabels(
  labels: readonly LabelInput[],
  options: LayoutOptions,
): Map<string, Offset> {
  const gap = options.gap ?? LAYOUT_GAP;
  const maxShift = options.maxShift ?? LAYOUT_MAX_SHIFT;
  const step = options.step ?? LAYOUT_STEP;
  const placed: Rect[] = [];
  const out = new Map<string, Offset>();

  const free = (rect: Rect) => placed.every((p) => !overlaps(rect, p));

  for (const label of labels) {
    let offset: Offset = { dx: 0, dy: 0 };
    let rect = rectAt(label, options.anchor, offset, gap);
    if (!free(rect)) {
      search: for (let distance = step; distance <= maxShift; distance += step) {
        for (const d of DIRECTIONS) {
          const candidate = { dx: Math.round(d.x * distance), dy: Math.round(d.y * distance) };
          const candidateRect = rectAt(label, options.anchor, candidate, gap);
          if (free(candidateRect)) {
            offset = candidate;
            rect = candidateRect;
            break search;
          }
        }
      }
    }
    placed.push(rect);
    out.set(label.key, offset);
  }
  return out;
}

/** Whether two layouts move every bubble by the same amount (±tolerance points). */
export function sameLayout(
  a: ReadonlyMap<string, Offset>,
  b: ReadonlyMap<string, Offset>,
  tolerance = 1,
): boolean {
  if (a.size !== b.size) return false;
  for (const [key, offset] of a) {
    const other = b.get(key);
    if (
      !other ||
      Math.abs(offset.dx - other.dx) > tolerance ||
      Math.abs(offset.dy - other.dy) > tolerance
    ) {
      return false;
    }
  }
  return true;
}

/** True when an offset is too small to need a leader line to its stop. */
export function isInPlace(offset: Offset | undefined, tolerance = 2): boolean {
  return !offset || (Math.abs(offset.dx) <= tolerance && Math.abs(offset.dy) <= tolerance);
}
