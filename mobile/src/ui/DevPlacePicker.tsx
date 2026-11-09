import { useState } from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';

import type { LatLng } from '@/core/geo';
import type { LocationStore } from '@/state/location';

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

const styles = StyleSheet.create({
  container: {
    position: 'absolute',
    top: 60,
    right: 12,
    alignItems: 'flex-end',
    pointerEvents: 'box-none',
    gap: 6,
  },
  toggle: {
    paddingVertical: 6,
    paddingHorizontal: 10,
    borderRadius: 8,
    backgroundColor: 'rgba(17, 17, 17, 0.8)',
  },
  toggleLabel: {
    color: '#ffffff',
    fontSize: 13,
    fontWeight: '600',
  },
  list: {
    borderRadius: 10,
    backgroundColor: 'rgba(255, 255, 255, 0.97)',
    paddingVertical: 4,
    shadowColor: '#000000',
    shadowOpacity: 0.2,
    shadowRadius: 4,
    shadowOffset: { width: 0, height: 1 },
    elevation: 4,
  },
  item: {
    paddingVertical: 8,
    paddingHorizontal: 14,
  },
  itemLabel: {
    fontSize: 15,
    color: '#111111',
  },
  pressed: {
    opacity: 0.6,
  },
});
