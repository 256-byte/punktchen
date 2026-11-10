// Keeps map markers from covering each other (task 7.9, ARCHITECTURE §4.5).
// After every camera move and whenever the markers or their sizes change, it
// asks the map where each marker's point is on the screen (the map's own
// projection, so pitch and rotation are right), lets layoutLabels move the
// bubbles apart, and turns the moved positions back into coordinates.
import { type RefObject, useCallback, useEffect, useRef, useState } from 'react';
import { Platform } from 'react-native';
import type MapView from 'react-native-maps';

import type { LatLng } from '@/core/geo';
import {
  type Anchor,
  isInPlace,
  type LabelInput,
  layoutLabels,
  type Offset,
  type Size,
} from '@/core/labelLayout';

/**
 * Where a marker's bubble sits on its point. Markers ask for the bottom
 * centre (`anchor={{ x: 0.5, y: 1 }}`), which Google Maps follows; Apple Maps
 * ignores `anchor` and centres the bubble (react-native-maps: "iOS: Google
 * Maps only").
 */
export const MARKER_ANCHOR: Anchor = Platform.OS === 'ios' ? { x: 0.5, y: 0.5 } : { x: 0.5, y: 1 };

/** Size assumed until a marker has reported its own (a 4-row bubble). */
export const DEFAULT_MARKER_SIZE: Size = { width: 170, height: 90 };

/** Waits this long after the last change before laying out once. */
export const LAYOUT_DEBOUNCE_MS = 40;

export interface LayoutItem {
  key: string;
  location: LatLng;
}

/** A moved marker: draw it at displayAt, its stop is at location. */
export interface Displaced {
  location: LatLng;
  displayAt: LatLng;
}

/**
 * items: the markers in priority order (first = never moved). Returns the
 * moved markers by key, `relayout` (call after the camera moved), and
 * `reportSize` (a marker's measured size).
 */
export function useMarkerLayout(map: RefObject<MapView | null>, items: readonly LayoutItem[]) {
  const [displaced, setDisplaced] = useState<ReadonlyMap<string, Displaced>>(() => new Map());
  const sizes = useRef(new Map<string, Size>());
  const itemsRef = useRef(items);
  const run = useRef(0);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const layout = useCallback(async () => {
    const view = map.current;
    const current = itemsRef.current;
    const id = ++run.current;
    if (!view || typeof view.pointForCoordinate !== 'function' || current.length === 0) {
      setDisplaced((prev) => (prev.size === 0 ? prev : new Map()));
      return;
    }
    let points;
    try {
      points = await Promise.all(
        current.map((i) =>
          view.pointForCoordinate({ latitude: i.location.lat, longitude: i.location.lng }),
        ),
      );
    } catch {
      return; // map not ready yet; the next camera move lays out again
    }
    if (id !== run.current) return; // a newer layout started

    const labels: LabelInput[] = [];
    current.forEach((item, i) => {
      const size = sizes.current.get(item.key) ?? DEFAULT_MARKER_SIZE;
      if (size.width > 0 && size.height > 0) {
        labels.push({ key: item.key, point: points[i]!, size });
      }
    });
    const offsets = layoutLabels(labels, { anchor: MARKER_ANCHOR });

    const moved: { item: LayoutItem; point: { x: number; y: number }; offset: Offset }[] = [];
    current.forEach((item, i) => {
      const offset = offsets.get(item.key);
      if (!isInPlace(offset)) moved.push({ item, point: points[i]!, offset: offset! });
    });
    let coordinates;
    try {
      coordinates = await Promise.all(
        moved.map(({ point, offset }) =>
          view.coordinateForPoint({ x: point.x + offset.dx, y: point.y + offset.dy }),
        ),
      );
    } catch {
      return;
    }
    if (id !== run.current) return;

    const next = new Map<string, Displaced>();
    moved.forEach(({ item }, i) => {
      const c = coordinates[i]!;
      next.set(item.key, {
        location: item.location,
        displayAt: { lat: c.latitude, lng: c.longitude },
      });
    });
    setDisplaced((prev) => (sameDisplaced(prev, next) ? prev : next));
  }, [map]);

  /** Lays out once the changes have settled (sizes arrive marker by marker). */
  const relayout = useCallback(() => {
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => {
      timer.current = null;
      void layout();
    }, LAYOUT_DEBOUNCE_MS);
  }, [layout]);

  const reportSize = useCallback(
    (key: string, size: Size) => {
      const known = sizes.current.get(key);
      if (
        known &&
        Math.abs(known.width - size.width) <= 2 &&
        Math.abs(known.height - size.height) <= 2
      ) {
        return;
      }
      sizes.current.set(key, size);
      relayout();
    },
    [relayout],
  );

  // New markers (zoom bucket, data, combine switch): lay out again.
  useEffect(() => {
    itemsRef.current = items;
    relayout();
  }, [items, relayout]);

  useEffect(
    () => () => {
      if (timer.current) clearTimeout(timer.current);
      run.current++; // drop a layout still waiting for the map
    },
    [],
  );

  return { displaced, relayout, reportSize };
}

/**
 * Same moved markers at the same places (within ~0.2 m, below a screen point
 * even at street zoom): a pan gives the same layout, and keeping the old map
 * avoids redrawing every marker.
 */
function sameDisplaced(a: ReadonlyMap<string, Displaced>, b: ReadonlyMap<string, Displaced>) {
  if (a.size !== b.size) return false;
  const EPS = 0.000002; // degrees, ≈ 0.2 m
  for (const [key, d] of b) {
    const o = a.get(key);
    if (
      !o ||
      Math.abs(o.displayAt.lat - d.displayAt.lat) > EPS ||
      Math.abs(o.displayAt.lng - d.displayAt.lng) > EPS
    ) {
      return false;
    }
  }
  return true;
}
