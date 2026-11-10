import { describe, expect, it } from 'vitest';

import { isInPlace, type LabelInput, layoutLabels, sameLayout } from './labelLayout';

const bottom = { anchor: { x: 0.5, y: 1 } }; // Google Maps: bubble above its point
const centre = { anchor: { x: 0.5, y: 0.5 } }; // Apple Maps: bubble centred on its point

const label = (key: string, x: number, y: number, width = 100, height = 40): LabelInput => ({
  key,
  point: { x, y },
  size: { width, height },
});

/** Screen rectangles of a layout, to check for overlaps. */
function rects(labels: LabelInput[], anchor: { x: number; y: number }) {
  const offsets = layoutLabels(labels, { anchor });
  return labels.map((l) => {
    const o = offsets.get(l.key)!;
    const left = l.point.x + o.dx - anchor.x * l.size.width;
    const top = l.point.y + o.dy - anchor.y * l.size.height;
    return { left, top, right: left + l.size.width, bottom: top + l.size.height };
  });
}

function anyOverlap(rs: ReturnType<typeof rects>) {
  return rs.some((a, i) =>
    rs.some(
      (b, j) =>
        i < j && a.left < b.right && b.left < a.right && a.top < b.bottom && b.top < a.bottom,
    ),
  );
}

describe('layoutLabels (task 7.9)', () => {
  it('leaves bubbles that do not touch where they are', () => {
    const offsets = layoutLabels([label('a', 100, 100), label('b', 300, 100)], bottom);
    expect(offsets.get('a')).toEqual({ dx: 0, dy: 0 });
    expect(offsets.get('b')).toEqual({ dx: 0, dy: 0 });
  });

  it('keeps the first bubble in place and moves the next one the shortest way out', () => {
    // Wide, flat bubbles: down (48) is shorter than right (84).
    const flat = layoutLabels([label('a', 100, 100), label('b', 120, 105)], bottom);
    expect(flat.get('a')).toEqual({ dx: 0, dy: 0 });
    expect(flat.get('b')).toEqual({ dx: 0, dy: 48 });
    // Narrow, tall bubbles: sideways is shorter.
    const tall = layoutLabels(
      [label('a', 100, 100, 40, 100), label('b', 105, 110, 40, 100)],
      bottom,
    );
    expect(tall.get('b')).toEqual({ dx: 48, dy: 0 });
  });

  it('prefers sideways over up or down at the same distance', () => {
    // Square bubbles on one point: right and down are equally far.
    const offsets = layoutLabels(
      [label('a', 100, 100, 40, 40), label('b', 100, 100, 40, 40)],
      bottom,
    );
    expect(offsets.get('b')).toEqual({ dx: 48, dy: 0 });
  });

  it.each([
    ['Google Maps (bottom anchor)', bottom.anchor],
    ['Apple Maps (centred)', centre.anchor],
  ])('separates a crowd of bubbles at one spot: %s', (_, anchor) => {
    // Alexanderplatz zoomed out: 12 stops within a few points.
    const crowd = Array.from({ length: 12 }, (_, i) =>
      label(`s${i}`, 400 + (i % 4) * 6, 400 + Math.floor(i / 4) * 5, 140, 60),
    );
    expect(anyOverlap(rects(crowd, anchor))).toBe(false);
  });

  it('gives up beyond maxShift and keeps the bubble at its point', () => {
    // b's point is deep inside a's bubble (y −400…600, x −400…600).
    const offsets = layoutLabels([label('a', 100, 600, 1000, 1000), label('b', 100, 100)], {
      ...bottom,
      maxShift: 50,
    });
    expect(offsets.get('b')).toEqual({ dx: 0, dy: 0 });
  });

  it('gives the same offsets after a pan (all points moved alike)', () => {
    const at = (dx: number, dy: number) =>
      layoutLabels(
        [
          label('a', 100 + dx, 100 + dy),
          label('b', 110 + dx, 100 + dy),
          label('c', 90 + dx, 95 + dy),
        ],
        bottom,
      );
    expect(sameLayout(at(0, 0), at(-300, 170))).toBe(true);
  });

  it('puts bubbles back in place once zoomed in (points far apart)', () => {
    const zoomedOut = layoutLabels([label('a', 100, 100), label('b', 110, 100)], bottom);
    expect(isInPlace(zoomedOut.get('b'))).toBe(false);
    const zoomedIn = layoutLabels([label('a', 100, 100), label('b', 400, 100)], bottom);
    expect(isInPlace(zoomedIn.get('b'))).toBe(true);
  });
});

describe('sameLayout / isInPlace', () => {
  it('compares offsets within a tolerance', () => {
    const a = new Map([['x', { dx: 10, dy: 0 }]]);
    expect(sameLayout(a, new Map([['x', { dx: 11, dy: -1 }]]))).toBe(true);
    expect(sameLayout(a, new Map([['x', { dx: 13, dy: 0 }]]))).toBe(false);
    expect(sameLayout(a, new Map([['y', { dx: 10, dy: 0 }]]))).toBe(false);
    expect(sameLayout(a, new Map())).toBe(false);
  });

  it('treats tiny or missing offsets as in place', () => {
    expect(isInPlace(undefined)).toBe(true);
    expect(isInPlace({ dx: 2, dy: -2 })).toBe(true);
    expect(isInPlace({ dx: 0, dy: 3 })).toBe(false);
  });
});
