// The transit layer (the only layer, CLAUDE.md rule 9). For now one plain
// pin per station; pole markers with departures come in task 3.5.
import type { components } from '@/api/schema.gen';

import { defineLayer } from '../registry';
import { StationMarker } from './StationMarker';

export type Station = components['schemas']['Station'];

export const transitLayer = defineLayer<Station>({
  id: 'transit',
  select: (response) => response.layers.transit?.features ?? [],
  key: (station) => station.id,
  Marker: StationMarker,
});
