import { Marker } from 'react-native-maps';

import type { Station } from './index';

/** A plain pin with the station name (replaced by pole markers in task 3.5). */
export function StationMarker({ feature }: { feature: Station }) {
  return (
    <Marker
      coordinate={{ latitude: feature.location.lat, longitude: feature.location.lng }}
      title={feature.name}
      tracksViewChanges={false}
    />
  );
}
