import { StyleSheet, Text, View } from 'react-native';

// Placeholder start screen. The map replaces it in Phase 3; task 0.8 adds
// the backend health check.
export default function HomeScreen() {
  return (
    <View style={styles.container}>
      <Text style={styles.title}>LiveStop</Text>
      <Text style={styles.subtitle}>Live departures around you</Text>
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
