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

/** Camera tilt angle in degrees when 3D view is enabled (task 7.4). */
export const PITCH_3D_DEG = 50;

/**
 * Maximum Apple Maps camera altitude (metres) and minimum Google Maps zoom
 * level at which 3D extruded buildings render on iOS MapKit and Android
 * Google Maps when switching to 3D (task 7.4).
 */
export const MAX_3D_ALTITUDE_M = 750;
export const MIN_3D_ZOOM = 16.5;

/** Camera target in react-native-maps' shape (`animateCamera`). */
export interface PitchCamera {
  center?: { latitude: number; longitude: number };
  pitch: number;
  heading: number;
  /** Apple Maps vertical camera distance in metres. */
  altitude?: number;
  /** Google Maps zoom level. */
  zoom?: number;
}

/**
 * Camera parameters for `animateCamera` when toggling between 2D (`pitched =
 * false`) and 3D (`pitched = true`, task 7.4).
 *
 * Both Apple Maps (`altitude` in metres) and Google Maps (`zoom`) need the
 * camera close enough to extrude 3D buildings (`altitude ≤ 750 m` on iOS,
 * `zoom ≥ 16.5` on Android). When the user is already zoomed in closer than
 * that, their current span (`latitudeDelta`) is preserved; when returning to
 * 2D, only `pitch` and `heading` reset to 0 so the current zoom stays intact.
 */
export function pitchCamera(pitched: boolean, latitudeDelta: number, center?: LatLng): PitchCamera {
  const base: PitchCamera = {
    ...(center ? { center: { latitude: center.lat, longitude: center.lng } } : {}),
    pitch: pitched ? PITCH_3D_DEG : 0,
    heading: 0,
  };
  if (!pitched) {
    return base;
  }
  const spanM = Math.max(150, latitudeDelta * METERS_PER_DEGREE);
  const altitude = Math.min(MAX_3D_ALTITUDE_M, Math.round(spanM));
  const currentZoom = Math.log2(360 / Math.max(0.0005, latitudeDelta));
  const zoom = Math.max(MIN_3D_ZOOM, Math.min(20, Number(currentZoom.toFixed(1))));
  return { ...base, altitude, zoom };
}
