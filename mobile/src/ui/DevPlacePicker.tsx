import { useState } from 'react';
import { Pressable, Text, View } from 'react-native';

import type { LatLng } from '@/core/geo';
import type { LocationStore } from '@/state/location';

import { themedStyles } from './theme';

/**
 * Places to jump to while testing on a phone (development builds only): the
 * stations of the VBB spot check (task 2.6). Coordinates from the dev DB's
 * gtfs.stations.
 */
export const DEV_PLACES: readonly { name: string; center: LatLng }[] = [
  { name: 'S+U Wedding', center: { lat: 52.54273, lng: 13.36606 } },
  { name: 'S+U Alexanderplatz', center: { lat: 52.52151, lng: 13.41127 } },
  { name: 'U Eberswalder Str.', center: { lat: 52.54109, lng: 13.41216 } },
  { name: 'S Wannsee', center: { lat: 52.42173, lng: 13.17893 } },
  { name: 'U Hermannplatz/Sonnenallee', center: { lat: 52.48749, lng: 13.4262 } },
];

/**
 * Developer tool: look around a chosen place instead of the device
 * position ("map center" mode at that place), or go back to it.
 */
export function DevPlacePicker({ store }: { store: LocationStore }) {
  const [open, setOpen] = useState(false);
  const styles = useStyles();
  const choose = (action: () => void) => {
    action();
    setOpen(false);
  };

  return (
    <View style={styles.container}>
      <Pressable
        accessibilityRole="button"
        onPress={() => setOpen((o) => !o)}
        style={({ pressed }) => [styles.toggle, pressed && styles.pressed]}
      >
        <Text style={styles.toggleLabel}>Dev: place</Text>
      </Pressable>
      {open ? (
        <View style={styles.list}>
          <Item label="My location" onPress={() => choose(() => store.getState().chooseDevice())} />
          {DEV_PLACES.map((p) => (
            <Item
              key={p.name}
              label={p.name}
              onPress={() => choose(() => store.getState().lookAt(p.center))}
            />
          ))}
        </View>
      ) : null}
    </View>
  );
}

function Item({ label, onPress }: { label: string; onPress: () => void }) {
  const styles = useStyles();
  return (
    <Pressable
      accessibilityRole="button"
      onPress={onPress}
      style={({ pressed }) => [styles.item, pressed && styles.pressed]}
    >
      <Text style={styles.itemLabel}>{label}</Text>
    </Pressable>
  );
}

const useStyles = themedStyles(({ colors, fontSize, fontWeight, opacity, radius, space }) => ({
  container: {
    position: 'absolute',
    top: 124, // below the status banner (task 6.2)
    right: space.lg,
    alignItems: 'flex-end',
    pointerEvents: 'box-none',
    gap: space.sm,
  },
  toggle: {
    paddingVertical: space.sm,
    paddingHorizontal: space.ml,
    borderRadius: radius.md,
    backgroundColor: colors.inverseSurface,
    opacity: 0.85,
  },
  toggleLabel: {
    color: colors.onInverse,
    fontSize: fontSize.footnote,
    fontWeight: fontWeight.semibold,
  },
  list: {
    borderRadius: radius.lg,
    backgroundColor: colors.surface,
    paddingVertical: space.xs,
    shadowColor: colors.shadow,
    shadowOpacity: 0.2,
    shadowRadius: 4,
    shadowOffset: { width: 0, height: 1 },
    elevation: 4,
  },
  item: {
    paddingVertical: space.md,
    paddingHorizontal: space.lg,
  },
  itemLabel: {
    fontSize: fontSize.body,
    color: colors.text,
  },
  pressed: {
    opacity: opacity.pressed,
  },
}));
