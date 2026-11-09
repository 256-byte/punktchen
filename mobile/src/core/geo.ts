// Geographic helpers.

export interface LatLng {
  lat: number;
  lng: number;
}

/** Mean earth radius (IUGG) in metres. */
const EARTH_RADIUS_M = 6_371_008.8;

const rad = (deg: number) => (deg * Math.PI) / 180;

/** Great-circle distance in metres (haversine); good to well under 1 m at city scale. */
export function distanceMeters(a: LatLng, b: LatLng): number {
  const dLat = rad(b.lat - a.lat);
  const dLng = rad(b.lng - a.lng);
  const h =
    Math.sin(dLat / 2) ** 2 + Math.cos(rad(a.lat)) * Math.cos(rad(b.lat)) * Math.sin(dLng / 2) ** 2;
  return 2 * EARTH_RADIUS_M * Math.asin(Math.min(1, Math.sqrt(h)));
}

/** A map region in react-native-maps' shape (centre and span in degrees). */
export interface Region {
  latitude: number;
  longitude: number;
  latitudeDelta: number;
  longitudeDelta: number;
}

/** Metres per degree of latitude. */
const METERS_PER_DEGREE = (EARTH_RADIUS_M * Math.PI) / 180;

/**
 * The region centred on a point that shows at least radiusM in every
 * direction (plus a 20 % margin), e.g. the /v1/nearby search circle.
 */
export function regionAround(center: LatLng, radiusM: number): Region {
  const latitudeDelta = (2.4 * radiusM) / METERS_PER_DEGREE;
  return {
    latitude: center.lat,
    longitude: center.lng,
    latitudeDelta,
    longitudeDelta: latitudeDelta / Math.cos(rad(center.lat)),
  };
}
