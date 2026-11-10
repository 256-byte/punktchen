// Mobile layer registry (ARCHITECTURE §4.4, ADR-0008). The map screen draws
// every registered layer through this interface and knows nothing about
// departures, stations or any other layer's features.
import type { ComponentType } from 'react';

import type { ApiClient } from '@/api/client';
import type { NearbyResponse } from '@/api/nearby';
import type { components } from '@/api/schema.gen';
import type { LatLng } from '@/core/geo';
import type { Size } from '@/core/labelLayout';

export type LayerId = components['schemas']['LayerId'];

/**
 * How the user wants the map drawn, from switches on the map screen. Layers
 * read what applies to them and ignore the rest.
 */
export interface MapViewOptions {
  /** Both platforms / sides of one stop in one marker (transit, task 7.8). */
  combineStops?: boolean;
}

export interface MapLayer<TFeature> {
  id: LayerId;
  /**
   * This layer's features in a /v1/nearby response (none if the layer is
   * down), clustered or filtered for the map's visible vertical span
   * (`latitudeDelta` in degrees; task 7.2) and the view options.
   */
  select(response: NearbyResponse, latitudeDelta?: number, options?: MapViewOptions): TFeature[];
  /** Stable React key of a feature. */
  key(feature: TFeature): string;
  /** Where a feature is on the map (its marker's point; task 7.9). */
  location(feature: TFeature): LatLng;
  /**
   * What the map draws for a feature (a react-native-maps Marker); onPress
   * opens its sheet. stale: the /v1/nearby data is older than 5 min, so
   * values that were live are shown as last known (ARCHITECTURE §4.3).
   * displayAt: where to draw the marker instead of location(feature), when
   * the map moved it aside so it does not cover another one (task 7.9).
   * onSize: the marker reports its drawn size (0 × 0 when it draws nothing),
   * which the map needs to keep markers apart.
   */
  Marker: ComponentType<{
    feature: TFeature;
    onPress: (feature: TFeature) => void;
    stale: boolean;
    displayAt?: LatLng;
    onSize?: (size: Size) => void;
  }>;
  /**
   * Content of the bottom sheet for a tapped feature (the map screen owns
   * the sheet). stale: as for Marker; for what the sheet shows from the
   * feature itself, before or without data of its own.
   */
  Sheet: ComponentType<{ feature: TFeature; client: ApiClient | null; stale: boolean }>;
}

/**
 * A layer as the registry keeps it: its feature type is hidden, since the
 * map screen only passes a layer's own features back to that layer.
 */
export type AnyMapLayer = MapLayer<unknown>;

export function defineLayer<TFeature>(layer: MapLayer<TFeature>): AnyMapLayer {
  return layer as unknown as AnyMapLayer;
}
