import { Pressable, StyleSheet, Text, View } from 'react-native';

import type { ApiClient } from '@/api/client';
import { useHealth } from '@/api/health';
import type { ApiBaseUrl } from '@/core/apiBaseUrl';

type Props = {
  baseUrl: ApiBaseUrl;
  client: ApiClient | null;
};

/** Shows whether the app reaches the backend (GET /v1/health). */
export function BackendStatus({ baseUrl, client }: Props) {
  if (!baseUrl.ok || client === null) {
    return (
      <View style={styles.box}>
        <Text style={[styles.heading, styles.bad]}>Backend not configured</Text>
        <Text style={styles.detail}>{baseUrl.ok ? 'no API client' : baseUrl.error}</Text>
        <Text style={styles.hint}>
          Set EXPO_PUBLIC_API_URL in mobile/.env (see README), then restart Expo.
        </Text>
      </View>
    );
  }
  return <HealthResult url={baseUrl.url} client={client} />;
}

function HealthResult({ url, client }: { url: string; client: ApiClient }) {
  const health = useHealth(client);

  return (
    <View style={styles.box}>
      {health.isPending ? (
        <Text style={styles.heading}>Checking backend…</Text>
      ) : health.isError ? (
        <>
          <Text style={[styles.heading, styles.bad]}>Backend unreachable</Text>
          <Text style={styles.detail}>{health.error.message}</Text>
          <Text style={styles.hint}>
            Phone on the same Wi-Fi as the PC? Backend running? Firewall open for port 8080?
          </Text>
        </>
      ) : (
        <>
          <Text style={[styles.heading, health.data.status === 'ok' ? styles.good : styles.bad]}>
            Backend: {health.data.status}
          </Text>
          <Text style={styles.detail}>version {health.data.version}</Text>
          <Text style={styles.detail}>server time {health.data.time}</Text>
        </>
      )}
      <Text style={styles.url}>{url}</Text>
      <Pressable
        accessibilityRole="button"
        onPress={() => void health.refetch()}
        disabled={health.isFetching}
        style={({ pressed }) => [styles.button, (pressed || health.isFetching) && styles.pressed]}
      >
        <Text style={styles.buttonText}>Check again</Text>
      </Pressable>
    </View>
  );
}

const styles = StyleSheet.create({
  box: {
    alignSelf: 'stretch',
    marginHorizontal: 24,
    marginTop: 32,
    padding: 16,
    borderRadius: 12,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: '#999',
    gap: 6,
  },
  heading: { fontSize: 18, fontWeight: '600' },
  good: { color: '#1a7f37' },
  bad: { color: '#cf222e' },
  detail: { fontSize: 14 },
  hint: { fontSize: 13, opacity: 0.7 },
  url: { fontSize: 12, opacity: 0.5, marginTop: 4 },
  button: {
    marginTop: 8,
    alignSelf: 'flex-start',
    paddingHorizontal: 14,
    paddingVertical: 8,
    borderRadius: 8,
    backgroundColor: '#0969da',
  },
  pressed: { opacity: 0.6 },
  buttonText: { color: '#fff', fontWeight: '600' },
});
