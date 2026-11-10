// The transit layer (the only layer, CLAUDE.md rule 9): one marker per pole
// or station cluster depending on zoom (tasks 3.5, 7.2); tapping it opens
// the station sheet (task 3.6).
import type { components } from '@/api/schema.gen';
import { clusterStations, type DetailLevel } from '@/core/clustering';

import { defineLayer } from '../registry';
import { PoleMarker } from './PoleMarker';
import { StationSheet } from './StationSheet';

export type Station = components['schemas']['Station'];
export type Pole = components['schemas']['Pole'];

/** A pole or zoom-clustered group of poles with its primary station. */
export interface PoleFeature {
  station: Station;
  pole: Pole;
  /** Number of active poles merged into this marker (default: 1). */
  poleCount?: number;
  /** Number of stations merged into this marker (default: 1). */
  stationCount?: number;
  /** Zoom detail level ('poles' | 'stations' | 'compact'; default: 'poles'). */
  detail?: DetailLevel;
}

export const transitLayer = defineLayer<PoleFeature>({
  id: 'transit',
  select: (response, latitudeDelta) =>
    clusterStations(response.layers.transit?.features ?? [], latitudeDelta),
  key: (f) => f.pole.id,
  Marker: PoleMarker,
  Sheet: StationSheet,
});
