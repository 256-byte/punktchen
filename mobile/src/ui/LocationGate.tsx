import type { ReactNode } from 'react';
import { ActivityIndicator, Pressable, StyleSheet, Text, View } from 'react-native';

import type { LatLng } from '@/core/geo';
import type { LocationView } from '@/core/location';
import { requestLocationAccess } from '@/lib/location';
import { locationStore, type LocationStore, useLocationView } from '@/state/location';

type Ready = Extract<LocationView, { kind: 'ready' }>;

type Props = {
  /** Rendered once there is a position to look around. */
  children: (ready: Ready) => ReactNode;
  /**
   * Where the kept /v1/nearby answer was fetched for (task 6.1): shown
   * while the permission or the first fix is pending, instead of waiting.
   */
  lastPlace?: LatLng | null;
  store?: LocationStore;
};

/**
 * Shows its children only with a position: the device's, or the map centre
 * the user chose. Without the location permission it explains why the app
 * asks and offers the map centre instead (task 3.3).
 */
export function LocationGate({ children, lastPlace = null, store = locationStore }: Props) {
  const view = useLocationView(store);
  const pickMapCenter = () => store.getState().chooseMapCenter();

  switch (view.kind) {
    case 'ready':
      return children(view);
    case 'checking':
      if (lastPlace !== null) {
        return children({ kind: 'ready', position: lastPlace, source: 'lastKnown' });
      }
      return (
        <View style={styles.container}>
          <ActivityIndicator />
          <Text style={styles.body}>Finding your location…</Text>
          <Button label="Use map center instead" onPress={pickMapCenter} secondary />
        </View>
      );
    case 'denied':
      return (
        <View style={styles.container}>
          <Text style={styles.title}>Location is off</Text>
          <Text style={styles.body}>
            LiveStop shows live departures at the stops around you. Allow location access, or look
            around the center of the map instead.
          </Text>
          {view.canAskAgain ? null : (
            <Text style={styles.hint}>
              Turn on location for LiveStop in the Settings app, then come back.
            </Text>
          )}
          <Button
            label={view.canAskAgain ? 'Allow location' : 'Open settings'}
            onPress={() => void requestLocationAccess(store)}
          />
          <Button label="Use map center" onPress={pickMapCenter} secondary />
        </View>
      );
  }
}

function Button({
  label,
  onPress,
  secondary = false,
}: {
  label: string;
  onPress: () => void;
  secondary?: boolean;
}) {
  return (
    <Pressable
      accessibilityRole="button"
      onPress={onPress}
      style={({ pressed }) => [
        styles.button,
        secondary && styles.buttonSecondary,
        pressed && styles.pressed,
      ]}
    >
      <Text style={[styles.buttonLabel, secondary && styles.buttonLabelSecondary]}>{label}</Text>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    gap: 12,
    padding: 32,
  },
  title: {
    fontSize: 22,
    fontWeight: '700',
  },
  body: {
    fontSize: 16,
    textAlign: 'center',
    opacity: 0.8,
  },
  hint: {
    fontSize: 14,
    textAlign: 'center',
    opacity: 0.6,
  },
  button: {
    minWidth: 220,
    alignItems: 'center',
    paddingVertical: 12,
    paddingHorizontal: 20,
    borderRadius: 10,
    backgroundColor: '#1f6feb',
  },
  buttonSecondary: {
    backgroundColor: 'transparent',
  },
  buttonLabel: {
    fontSize: 16,
    fontWeight: '600',
    color: '#ffffff',
  },
  buttonLabelSecondary: {
    color: '#1f6feb',
  },
  pressed: {
    opacity: 0.6,
  },
});
