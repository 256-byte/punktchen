import { StyleSheet, Text, View } from 'react-native';

import { apiBaseUrl, apiClient } from '@/lib/api';
import { BackendStatus } from '@/ui/BackendStatus';

// Placeholder start screen. The map replaces it in Phase 3.
export default function HomeScreen() {
  return (
    <View style={styles.container}>
      <Text style={styles.title}>LiveStop</Text>
      <Text style={styles.subtitle}>Live departures around you</Text>
      <BackendStatus baseUrl={apiBaseUrl} client={apiClient} />
    </View>
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
});
