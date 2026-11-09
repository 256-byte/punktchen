// Mobile layer registry (ARCHITECTURE §4.4, ADR-0008). The map screen draws
// every registered layer through this interface and knows nothing about
// departures, stations or any other layer's features.
import type { ComponentType } from 'react';

import type { ApiClient } from '@/api/client';
import type { NearbyResponse } from '@/api/nearby';
import type { components } from '@/api/schema.gen';

export type LayerId = components['schemas']['LayerId'];

export interface MapLayer<TFeature> {
  id: LayerId;
  /** This layer's features in a /v1/nearby response (none if the layer is down). */
  select(response: NearbyResponse): TFeature[];
  /** Stable React key of a feature. */
  key(feature: TFeature): string;
  /** What the map draws for a feature (a react-native-maps Marker); onPress opens its sheet. */
  Marker: ComponentType<{ feature: TFeature; onPress: (feature: TFeature) => void }>;
  /** Content of the bottom sheet for a tapped feature (the map screen owns the sheet). */
  Sheet: ComponentType<{ feature: TFeature; client: ApiClient | null }>;
}

/**
 * A layer as the registry keeps it: its feature type is hidden, since the
 * map screen only passes a layer's own features back to that layer.
 */
export type AnyMapLayer = MapLayer<unknown>;

export function defineLayer<TFeature>(layer: MapLayer<TFeature>): AnyMapLayer {
  return layer as unknown as AnyMapLayer;
}
