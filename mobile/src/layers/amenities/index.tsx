// The amenity layers (Phase 9, ADR-0010): public toilets and drinking
// fountains, one marker per feature; tapping it opens its details. Each has
// a switch on the map.
import type { ComponentProps } from 'react';

import {
  type Fountain,
  fountainLabel,
  fountainStatus,
  type Toilet,
  toiletLabel,
} from '@/core/amenities';
import { clockStore, useNow } from '@/state/clock';

import { defineLayer, type MapLayer } from '../registry';
import { AmenityMarker } from './AmenityMarker';
import { FountainSheet, ToiletSheet } from './AmenitySheet';

type MarkerProps<T> = ComponentProps<MapLayer<T>['Marker']>;

function ToiletMarker({ feature: t, onPress, displayAt, onSize }: MarkerProps<Toilet>) {
  return (
    <AmenityMarker
      kind="toilet"
      at={t.location}
      displayAt={displayAt}
      // Only open at events or funerals: usable on few days.
      dimmed={t.eventOnly}
      label={toiletLabel(t)}
      onPress={() => onPress(t)}
      onSize={onSize}
    />
  );
}

function FountainMarker({ feature: f, onPress, displayAt, onSize }: MarkerProps<Fountain>) {
  // Plain values from the clock: re-renders only when the verdict changes.
  const label = useNow((now) => fountainLabel(f, now), clockStore);
  const running = useNow((now) => fountainStatus(f, now).state === 'running', clockStore);
  return (
    <AmenityMarker
      kind="fountain"
      at={f.location}
      displayAt={displayAt}
      dimmed={!running}
      label={label}
      onPress={() => onPress(f)}
      onSize={onSize}
    />
  );
}

export const toiletsLayer = defineLayer<Toilet>({
  id: 'toilets',
  toggle: { label: 'WC', accessibilityLabel: 'Public toilets' },
  select: (response) => response.layers.toilets?.features ?? [],
  key: (t) => t.id,
  location: (t) => t.location,
  Marker: ToiletMarker,
  Sheet: ToiletSheet,
});

export const fountainsLayer = defineLayer<Fountain>({
  id: 'fountains',
  toggle: { label: 'Water', accessibilityLabel: 'Drinking fountains' },
  select: (response) => response.layers.fountains?.features ?? [],
  key: (f) => f.id,
  location: (f) => f.location,
  Marker: FountainMarker,
  Sheet: FountainSheet,
});
