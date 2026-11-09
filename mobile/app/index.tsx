import { StyleSheet, Text, View } from 'react-native';

import { apiBaseUrl, apiClient } from '@/lib/api';
import { useDeviceLocation } from '@/lib/location';
import { BackendStatus } from '@/ui/BackendStatus';
import { LocationGate } from '@/ui/LocationGate';

const SOURCE_LABEL = {
  live: 'your location',
  lastKnown: 'your last known location',
  mapCenter: 'map center',
} as const;

// Placeholder start screen. The map replaces it in task 3.4.
export default function HomeScreen() {
  useDeviceLocation();
  return (
    <LocationGate>
      {({ position, source }) => (
        <View style={styles.container}>
          <Text style={styles.title}>LiveStop</Text>
          <Text style={styles.subtitle}>Live departures around you</Text>
          <Text style={styles.position}>
            Looking around {SOURCE_LABEL[source]}: {position.lat.toFixed(5)},{' '}
            {position.lng.toFixed(5)}
          </Text>
          <BackendStatus baseUrl={apiBaseUrl} client={apiClient} />
        </View>
      )}
    </LocationGate>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    gap: 8,
  },
  title: {
    fontSize: 32,
    fontWeight: '700',
  },
  subtitle: {
    fontSize: 16,
    opacity: 0.7,
  },
  position: {
    fontSize: 14,
    opacity: 0.7,
  },
});
