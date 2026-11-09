// The layers the map draws, in drawing order.
import type { AnyMapLayer } from './registry';
import { transitLayer } from './transit';

export const mapLayers: readonly AnyMapLayer[] = [transitLayer];
