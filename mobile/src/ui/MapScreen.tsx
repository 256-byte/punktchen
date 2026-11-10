import { useEffect, useRef, useState } from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';
import BottomSheet from '@gorhom/bottom-sheet';
import MapView from 'react-native-maps';
import { useStore } from 'zustand';

import { type ApiClient, failureOf } from '@/api/client';
import type { NearbyResponse } from '@/api/nearby';
import { useNearby } from '@/api/useNearby';
import type { ApiBaseUrl } from '@/core/apiBaseUrl';
import { COMBINE_STOPS_DEFAULT, sameZoomSpec } from '@/core/clustering';
import { nearbyEmptyState } from '@/core/directions';
import { nextRequestCenter } from '@/core/fetchPolicy';
import { type LatLng, pitchCamera, regionAround } from '@/core/geo';
import type { LocationView } from '@/core/location';
import { freshness } from '@/core/staleness';
import { mapLayers } from '@/layers';
import type { AnyMapLayer, MapViewOptions } from '@/layers/registry';
import { hapticLight, hapticSelection } from '@/lib/haptics';
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
  const [latitudeDelta, setLatitudeDelta] = useState(initialRegion.latitudeDelta);
  const [pitched, setPitched] = useState(false);
  const [combineStops, setCombineStops] = useState(COMBINE_STOPS_DEFAULT);
  const pitchedRef = useRef(false);
  const deltaRef = useRef(initialRegion.latitudeDelta);
  const following = useRef(true);
  const followsDevice = source !== 'mapCenter';

  // Back to the device position (e.g. "My location" in the dev picker): follow again.
  useEffect(() => {
    if (followsDevice) following.current = true;
  }, [followsDevice]);

  useEffect(() => {
    if (followsDevice && following.current) {
      if (pitchedRef.current) {
        map.current?.animateCamera(pitchCamera(true, deltaRef.current, position), {
          duration: FOLLOW_ANIMATION_MS,
        });
      } else {
        map.current?.animateToRegion(regionAround(position, START_RADIUS_M), FOLLOW_ANIMATION_MS);
      }
    }
  }, [followsDevice, position]);

  // A place chosen with lookAt (dev place picker): move there once.
  const jump = useStore(store, (s) => s.jump);
  useEffect(() => {
    if (jump) {
      if (pitchedRef.current) {
        map.current?.animateCamera(pitchCamera(true, deltaRef.current, jump.center), {
          duration: FOLLOW_ANIMATION_MS,
        });
      } else {
        map.current?.animateToRegion(
          regionAround(jump.center, START_RADIUS_M),
          FOLLOW_ANIMATION_MS,
        );
      }
    }
  }, [jump]);

  const togglePitch = () => {
    hapticLight();
    const next = !pitched;
    pitchedRef.current = next;
    setPitched(next);
    map.current?.animateCamera(pitchCamera(next, deltaRef.current), {
      duration: FOLLOW_ANIMATION_MS,
    });
  };

  const toggleCombineStops = () => {
    hapticLight();
    setCombineStops((on) => !on);
  };

  const selectFeature = (next: Selected) => {
    hapticSelection();
    setSelected(next);
  };

  const retryNearby = () => {
    hapticLight();
    nearby.refresh();
  };

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
  const emptyState = nearby.data
    ? nearbyEmptyState(nearby.data.response.layers.transit?.features ?? [])
    : null;
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
        showsBuildings
        pitchEnabled
        onPanDrag={() => {
          following.current = false;
        }}
        onRegionChangeComplete={(r) => {
          deltaRef.current = r.latitudeDelta;
          store.getState().setMapCenter({ lat: r.latitude, lng: r.longitude });
          setLatitudeDelta((prev) =>
            sameZoomSpec(prev, r.latitudeDelta) ? prev : r.latitudeDelta,
          );
        }}
      >
        {nearby.data ? (
          <LayerMarkers
            layers={layers}
            response={nearby.data.response}
            latitudeDelta={latitudeDelta}
            combineStops={combineStops}
            stale={stale}
            onSelect={selectFeature}
          />
        ) : null}
      </MapView>
      <MapAttribution lines={nearby.data?.response.meta.attribution ?? []} />
      <PitchToggle pitched={pitched} onToggle={togglePitch} />
      <CombineStopsToggle on={combineStops} onToggle={toggleCombineStops} />
      {devTools ? <DevPlacePicker store={store} /> : null}
      {client === null ? (
        <View style={styles.status}>
          <BackendStatus baseUrl={baseUrl} client={client} />
        </View>
      ) : checkedAt !== undefined ? (
        // With data on the map a failure is a line at the top, not a dialog.
        <>
          <StatusBanner
            checkedAt={checkedAt}
            failure={failure}
            onRetry={retryNearby}
            retrying={nearby.isFetching}
            clock={clock}
          />
          {emptyState ? (
            <View style={styles.status} accessibilityRole="alert" accessibilityLiveRegion="polite">
              <Text style={styles.emptyHeading}>{emptyState.title}</Text>
              <Text style={styles.statusDetail}>{emptyState.detail}</Text>
            </View>
          ) : null}
        </>
      ) : nearby.isError ? (
        <View style={styles.status} accessibilityRole="alert" accessibilityLiveRegion="polite">
          <Text style={styles.statusHeading}>Could not load departures</Text>
          <Text style={styles.statusDetail}>{nearby.error.message}</Text>
          <Pressable
            accessibilityRole="button"
            onPress={retryNearby}
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
  latitudeDelta,
  combineStops,
  stale,
  onSelect,
}: {
  layers: readonly AnyMapLayer[];
  response: NearbyResponse;
  latitudeDelta: number;
  combineStops: boolean;
  stale: boolean;
  onSelect: (selected: Selected) => void;
}) {
  const options: MapViewOptions = { combineStops };
  return layers.map((layer) =>
    layer
      .select(response, latitudeDelta, options)
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

function PitchToggle({ pitched, onToggle }: { pitched: boolean; onToggle: () => void }) {
  const styles = useStyles();
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel="3D"
      accessibilityHint="Tilts the map to show 3D buildings"
      accessibilityState={{ selected: pitched }}
      onPress={onToggle}
      style={({ pressed }) => [
        styles.pitchButton,
        pitched && styles.pitchButtonActive,
        pressed && styles.pressed,
      ]}
    >
      <Text style={[styles.pitchLabel, pitched && styles.pitchLabelActive]}>3D</Text>
    </Pressable>
  );
}

/**
 * Switch for combined stops (task 7.8, user request 2026-11-10): on, both
 * platforms / sides of one stop share a marker at street zoom; off, every
 * pole has its own, as before. Not saved: the app starts with the default.
 */
function CombineStopsToggle({ on, onToggle }: { on: boolean; onToggle: () => void }) {
  const styles = useStyles();
  return (
    <Pressable
      accessibilityRole="switch"
      accessibilityLabel="Combine stops"
      accessibilityHint="Shows both platforms or directions of a stop in one marker"
      accessibilityState={{ checked: on }}
      onPress={onToggle}
      style={({ pressed }) => [
        styles.pitchButton,
        styles.combineButton,
        on && styles.pitchButtonActive,
        pressed && styles.pressed,
      ]}
    >
      <Text style={[styles.pitchLabel, on && styles.pitchLabelActive]}>Combine</Text>
    </Pressable>
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

const useStyles = themedStyles(
  ({ scheme, colors, fontSize, fontWeight, opacity, radius, space }) => ({
    container: {
      flex: 1,
      backgroundColor: colors.background,
    },
    pitchButton: {
      position: 'absolute',
      top: 124, // aligned with DevPlacePicker below the status banner
      left: space.lg,
      paddingVertical: space.sm,
      paddingHorizontal: space.ml,
      borderRadius: radius.md,
      backgroundColor: colors.surface,
      borderWidth: scheme === 'dark' ? StyleSheet.hairlineWidth : 0,
      borderColor: colors.border,
      shadowColor: colors.shadow,
      shadowOpacity: 0.2,
      shadowRadius: 3,
      shadowOffset: { width: 0, height: 1 },
      elevation: 3,
    },
    combineButton: {
      top: 172, // below the 3D toggle
    },
    pitchButtonActive: {
      backgroundColor: colors.accent,
      borderColor: colors.accent,
    },
    pitchLabel: {
      fontSize: fontSize.footnote,
      fontWeight: fontWeight.bold,
      color: colors.text,
    },
    pitchLabelActive: {
      color: colors.onAccent,
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
    emptyHeading: {
      fontSize: fontSize.bodyLarge,
      fontWeight: fontWeight.semibold,
      color: colors.text,
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
  }),
);
