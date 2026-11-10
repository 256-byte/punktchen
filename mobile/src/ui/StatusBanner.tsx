import { Pressable, StyleSheet, Text, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { type Failure, statusBanner } from '@/core/staleness';
import { type ClockStore, clockStore, useDeviceNow } from '@/state/clock';

type Props = {
  /** Device time of the shown data's last 200 or 304 (`Kept.checkedAt`). */
  checkedAt: number;
  /** Why the last request failed; null if it worked. */
  failure: Failure | null;
  /** "Try again", offered while requests fail. */
  onRetry: () => void;
  retrying: boolean;
  clock?: ClockStore;
};

/**
 * How far the shown data can be trusted (task 6.2, ARCHITECTURE §4.3), at
 * the top of the map: nothing while it is fresh; "Updated 3 min ago" from
 * 60 s; a warning from 5 min; and "No connection — showing last known
 * data" as soon as a request fails. Re-renders when its text changes (once
 * a minute), not on every tick.
 */
export function StatusBanner({ checkedAt, failure, onRetry, retrying, clock = clockStore }: Props) {
  const insets = useSafeAreaInsets();
  const banner = useDeviceNow((now) => statusBanner(checkedAt, now, failure), clock);
  if (banner === null) {
    return null;
  }
  return (
    <View style={[styles.container, { top: insets.top + 8 }]}>
      <View
        style={[styles.banner, banner.warning && styles.warning]}
        accessibilityRole="alert"
        accessibilityLiveRegion="polite"
      >
        <View style={styles.text}>
          <Text style={[styles.title, banner.warning && styles.warningText]}>{banner.title}</Text>
          {banner.detail ? (
            <Text style={[styles.detail, banner.warning && styles.warningText]}>
              {banner.detail}
            </Text>
          ) : null}
        </View>
        {failure !== null ? (
          <Pressable
            accessibilityRole="button"
            onPress={onRetry}
            disabled={retrying}
            hitSlop={8}
            style={({ pressed }) => [styles.retry, (pressed || retrying) && styles.pressed]}
          >
            <Text style={styles.retryLabel}>Try again</Text>
          </Pressable>
        ) : null}
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    position: 'absolute',
    left: 12,
    right: 12,
    alignItems: 'center',
    pointerEvents: 'box-none',
  },
  banner: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
    maxWidth: '100%',
    paddingVertical: 6,
    paddingHorizontal: 12,
    borderRadius: 10,
    backgroundColor: 'rgba(255, 255, 255, 0.95)',
    shadowColor: '#000000',
    shadowOpacity: 0.15,
    shadowRadius: 3,
    shadowOffset: { width: 0, height: 1 },
    elevation: 2,
  },
  warning: {
    backgroundColor: '#fffaeb',
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: '#b54708',
  },
  text: {
    flexShrink: 1,
  },
  title: {
    fontSize: 13,
    fontWeight: '600',
    color: '#333333',
  },
  detail: {
    fontSize: 12,
    color: '#555555',
  },
  warningText: {
    color: '#93370d',
  },
  retry: {
    paddingVertical: 6,
    paddingHorizontal: 10,
    borderRadius: 8,
    backgroundColor: '#1f6feb',
  },
  retryLabel: {
    fontSize: 13,
    fontWeight: '600',
    color: '#ffffff',
  },
  pressed: {
    opacity: 0.6,
  },
});
