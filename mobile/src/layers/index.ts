// The layers the map draws, in drawing order.
// Transit first: it wins when markers have to move aside (task 7.9).
import { fountainsLayer, toiletsLayer } from './amenities';
import type { AnyMapLayer } from './registry';
import { transitLayer } from './transit';

export const mapLayers: readonly AnyMapLayer[] = [transitLayer, toiletsLayer, fountainsLayer];
