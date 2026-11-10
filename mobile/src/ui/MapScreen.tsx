import { useEffect, useRef, useState } from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';
import BottomSheet from '@gorhom/bottom-sheet';
import MapView from 'react-native-maps';
import { useStore } from 'zustand';

import { type ApiClient, failureOf } from '@/api/client';
import type { NearbyResponse } from '@/api/nearby';
import { useNearby } from '@/api/useNearby';
import type { ApiBaseUrl } from '@/core/apiBaseUrl';
import { nextRequestCenter } from '@/core/fetchPolicy';
import { type LatLng, regionAround } from '@/core/geo';
import type { LocationView } from '@/core/location';
import { freshness } from '@/core/staleness';
import { mapLayers } from '@/layers';
import type { AnyMapLayer } from '@/layers/registry';
import { type ClockStore, clockStore, useClockTicker, useDeviceNow } from '@/state/clock';
import { locationStore, type LocationStore } from '@/state/location';

import { MapAttribution } from './Attribution';
import { BackendStatus } from './BackendStatus';
import { DevPlacePicker } from './DevPlacePicker';
import { StatusBanner } from './StatusBanner';
import { themedStyles, useTheme } from './theme';

/** The map opens showing this radius around the user (the /v1/nearby default). */
export const START_RADIUS_M = 500;
const FOLLOW_ANIMATION_MS = 400;

type Props = {
  position: LatLng;
  source: Extract<LocationView, { kind: 'ready' }>['source'];
  baseUrl: ApiBaseUrl;
  client: ApiClient | null;
  layers?: readonly AnyMapLayer[];
  store?: LocationStore;
  clock?: ClockStore;
  /** Developer tools (place picker); default: only in development builds. */
  devTools?: boolean;
};

/**
 * The map (task 3.4). Opens centred on the position and follows the device
 * until the user pans; reports its centre to the location store (used in
 * "map center" mode); draws the registered layers' features from
 * /v1/nearby around the request centre; runs the 1 s clock ticker for
 * the countdowns (task 3.5). Says how old the data is and when requests
 * fail (`StatusBanner`), and tells the layers when it is stale (task 6.2).
 * Map, sheet and boxes follow the system's light or dark mode (task 7.1).
 */
export function MapScreen({
  position,
  source,
  baseUrl,
  client,
  layers = mapLayers,
  store = locationStore,
  clock = clockStore,
  devTools = __DEV__,
}: Props) {
  const map = useRef<MapView>(null);
  const [initialRegion] = useState(() => regionAround(position, START_RADIUS_M));
  const following = useRef(true);
  const followsDevice = source !== 'mapCenter';

  // Back to the device position (e.g. "My location" in the dev picker): follow again.
  useEffect(() => {
    if (followsDevice) following.current = true;
  }, [followsDevice]);

  useEffect(() => {
    if (followsDevice && following.current) {
      map.current?.animateToRegion(regionAround(position, START_RADIUS_M), FOLLOW_ANIMATION_MS);
    }
  }, [followsDevice, position]);

  // A place chosen with lookAt (dev place picker): move there once.
  const jump = useStore(store, (s) => s.jump);
  useEffect(() => {
    if (jump) {
      map.current?.animateToRegion(regionAround(jump.center, START_RADIUS_M), FOLLOW_ANIMATION_MS);
    }
  }, [jump]);

  const center = useRequestCenter(position);
  const nearby = useNearby(client, { lat: center.lat, lng: center.lng });
  const [selected, setSelected] = useState<Selected | null>(null);

  // Countdowns: one global ticker, corrected by the server clock.
  useClockTicker(clock);
  const offsetMs = nearby.data?.clockOffsetMs ?? 0;
  useEffect(() => clock.getState().setOffset(offsetMs), [clock, offsetMs]);

  // Data age (ARCHITECTURE §4.3). Changes when the 5 min line is crossed,
  // so the markers do not re-render with the clock.
  const checkedAt = nearby.data?.checkedAt;
  const stale = useDeviceNow(
    (now) => checkedAt !== undefined && freshness(checkedAt, now) === 'stale',
    clock,
  );
  const failure = nearby.isError ? failureOf(nearby.error) : null;
  const styles = useStyles();
  const { scheme } = useTheme();

  return (
    <View style={styles.container}>
      <MapView
        ref={map}
        style={StyleSheet.absoluteFill}
        initialRegion={initialRegion}
        // Light or dark map with the system (task 7.1): Apple Maps and Google Maps.
        userInterfaceStyle={scheme}
        showsUserLocation={followsDevice}
        onPanDrag={() => {
          following.current = false;
        }}
        onRegionChangeComplete={(r) =>
          store.getState().setMapCenter({ lat: r.latitude, lng: r.longitude })
        }
      >
        {nearby.data ? (
          <LayerMarkers
            layers={layers}
            response={nearby.data.response}
            stale={stale}
            onSelect={setSelected}
          />
        ) : null}
      </MapView>
      <MapAttribution lines={nearby.data?.response.meta.attribution ?? []} />
      {devTools ? <DevPlacePicker store={store} /> : null}
      {client === null ? (
        <View style={styles.status}>
          <BackendStatus baseUrl={baseUrl} client={client} />
        </View>
      ) : checkedAt !== undefined ? (
        // With data on the map a failure is a line at the top, not a dialog.
        <StatusBanner
          checkedAt={checkedAt}
          failure={failure}
          onRetry={nearby.refresh}
          retrying={nearby.isFetching}
          clock={clock}
        />
      ) : nearby.isError ? (
        <View style={styles.status}>
          <Text style={styles.statusHeading}>Could not load departures</Text>
          <Text style={styles.statusDetail}>{nearby.error.message}</Text>
          <Pressable
            accessibilityRole="button"
            onPress={nearby.refresh}
            disabled={nearby.isFetching}
            style={({ pressed }) => [
              styles.retry,
              (pressed || nearby.isFetching) && styles.pressed,
            ]}
          >
            <Text style={styles.retryLabel}>Try again</Text>
          </Pressable>
        </View>
      ) : null}
      {selected ? (
        <FeatureSheet
          selected={selected}
          client={client}
          stale={stale}
          onClose={() => setSelected(null)}
        />
      ) : null}
    </View>
  );
}

function LayerMarkers({
  layers,
  response,
  stale,
  onSelect,
}: {
  layers: readonly AnyMapLayer[];
  response: NearbyResponse;
  stale: boolean;
  onSelect: (selected: Selected) => void;
}) {
  return layers.map((layer) =>
    layer
      .select(response)
      .map((feature) => (
        <layer.Marker
          key={`${layer.id}:${layer.key(feature)}`}
          feature={feature}
          stale={stale}
          onPress={(f) => onSelect({ layer, feature: f })}
        />
      )),
  );
}

/** A tapped feature, shown in the bottom sheet. */
interface Selected {
  layer: AnyMapLayer;
  feature: unknown;
}

const SHEET_SNAP_POINTS = ['45%', '90%'];

/**
 * The bottom sheet for a tapped feature (task 3.6): the layer draws the
 * content; swiping down closes it.
 */
function FeatureSheet({
  selected,
  client,
  stale,
  onClose,
}: {
  selected: Selected;
  client: ApiClient | null;
  stale: boolean;
  onClose: () => void;
}) {
  const { layer, feature } = selected;
  const styles = useStyles();
  return (
    <BottomSheet
      index={0}
      snapPoints={SHEET_SNAP_POINTS}
      enablePanDownToClose
      onClose={onClose}
      backgroundStyle={styles.sheetBackground}
      handleIndicatorStyle={styles.sheetHandle}
    >
      <layer.Sheet
        key={`${layer.id}:${layer.key(feature)}`}
        feature={feature}
        client={client}
        stale={stale}
      />
    </BottomSheet>
  );
}

/** Request centre that moves only after > 150 m (nextRequestCenter). */
function useRequestCenter(position: LatLng): LatLng {
  const [center, setCenter] = useState(position);
  const next = nextRequestCenter(center, position);
  if (next !== center) {
    setCenter(next); // derived state, updated during render
  }
  return next;
}

const useStyles = themedStyles(({ colors, fontSize, fontWeight, opacity, radius, space }) => ({
  container: {
    flex: 1,
    backgroundColor: colors.background,
  },
  status: {
    position: 'absolute',
    left: space.xl,
    right: space.xl,
    bottom: space.xxxl,
    padding: space.xl,
    borderRadius: radius.xl,
    backgroundColor: colors.overlay,
    gap: space.sm,
  },
  statusHeading: {
    fontSize: fontSize.bodyLarge,
    fontWeight: fontWeight.semibold,
    color: colors.danger,
  },
  statusDetail: {
    fontSize: fontSize.callout,
    color: colors.textSecondary,
  },
  retry: {
    alignSelf: 'flex-start',
    marginTop: space.xs,
    paddingVertical: space.md,
    paddingHorizontal: space.lg,
    borderRadius: radius.md,
    backgroundColor: colors.accent,
  },
  retryLabel: {
    color: colors.onAccent,
    fontWeight: fontWeight.semibold,
  },
  pressed: {
    opacity: opacity.pressed,
  },
  sheetBackground: {
    backgroundColor: colors.surface,
  },
  sheetHandle: {
    backgroundColor: colors.handle,
  },
}));
