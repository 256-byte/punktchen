import { Pressable, StyleSheet, Text, View } from 'react-native';

import type { ApiClient } from '@/api/client';
import { useHealth } from '@/api/health';
import type { ApiBaseUrl } from '@/core/apiBaseUrl';

import { themedStyles } from './theme';

type Props = {
  baseUrl: ApiBaseUrl;
  client: ApiClient | null;
};

/** Shows whether the app reaches the backend (GET /v1/health). */
export function BackendStatus({ baseUrl, client }: Props) {
  const styles = useStyles();
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
  const styles = useStyles();

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

const useStyles = themedStyles(({ colors, fontSize, fontWeight, opacity, radius, space }) => ({
  box: {
    alignSelf: 'stretch',
    marginHorizontal: space.xxl,
    marginTop: space.xxxl,
    padding: space.xl,
    borderRadius: radius.xl,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.border,
    backgroundColor: colors.surface,
    gap: space.sm,
  },
  heading: { fontSize: fontSize.headline, fontWeight: fontWeight.semibold, color: colors.text },
  good: { color: colors.success },
  bad: { color: colors.danger },
  detail: { fontSize: fontSize.callout, color: colors.text },
  hint: { fontSize: fontSize.footnote, color: colors.textSecondary },
  url: { fontSize: fontSize.caption, color: colors.textTertiary, marginTop: space.xs },
  button: {
    marginTop: space.md,
    alignSelf: 'flex-start',
    paddingHorizontal: space.lg,
    paddingVertical: space.md,
    borderRadius: radius.md,
    backgroundColor: colors.accent,
  },
  pressed: { opacity: opacity.pressed },
  buttonText: { color: colors.onAccent, fontWeight: fontWeight.semibold },
}));
