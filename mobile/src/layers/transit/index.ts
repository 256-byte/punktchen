// The transit layer (the only layer, CLAUDE.md rule 9): one marker per pole
// with the next departure of each direction (task 3.5).
import type { components } from '@/api/schema.gen';

import { defineLayer } from '../registry';
import { PoleMarker } from './PoleMarker';

export type Station = components['schemas']['Station'];
export type Pole = components['schemas']['Pole'];

/** A pole with its station (name for the sheet and accessibility). */
export interface PoleFeature {
  station: Station;
  pole: Pole;
}

export const transitLayer = defineLayer<PoleFeature>({
  id: 'transit',
  select: (response) =>
    (response.layers.transit?.features ?? []).flatMap((station) =>
      station.poles.map((pole) => ({ station, pole })),
    ),
  key: (f) => f.pole.id,
  Marker: PoleMarker,
});
