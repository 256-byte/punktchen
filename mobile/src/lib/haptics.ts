// Haptic feedback for map interactions (task 7.5). Uses Expo's built-in
// native ExpoHaptics module when available on the device (iOS Taptic Engine /
// Android vibrator) and safely no-ops in tests or unsupported environments.
import { requireOptionalNativeModule } from 'expo-modules-core';

interface ExpoHapticsModule {
  selectionAsync?: () => Promise<void>;
  impactAsync?: (style: 'light' | 'medium' | 'heavy' | 'soft' | 'rigid') => Promise<void>;
  notificationAsync?: (type: 'success' | 'warning' | 'error') => Promise<void>;
}

function getHapticsModule(): ExpoHapticsModule | null {
  try {
    return requireOptionalNativeModule<ExpoHapticsModule>('ExpoHaptics');
  } catch {
    return null;
  }
}

/** Subtle tick when selecting a marker or option. */
export function hapticSelection(): void {
  try {
    void getHapticsModule()
      ?.selectionAsync?.()
      ?.catch(() => undefined);
  } catch {
    // Haptics are best-effort; ignore unsupported environments.
  }
}

/** Light tap when toggling a control or retrying a request. */
export function hapticLight(): void {
  try {
    void getHapticsModule()
      ?.impactAsync?.('light')
      ?.catch(() => undefined);
  } catch {
    // Haptics are best-effort; ignore unsupported environments.
  }
}
