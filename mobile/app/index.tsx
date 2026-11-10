import { useIsRestoring, useQueryClient } from '@tanstack/react-query';
import { useMemo } from 'react';

import { lastNearbyCenter } from '@/api/persistence';
import { apiBaseUrl, apiClient } from '@/lib/api';
import { useDeviceLocation } from '@/lib/location';
import { LocationGate } from '@/ui/LocationGate';
import { MapScreen } from '@/ui/MapScreen';

// Start screen: the map once there is a position (device or map centre).
// After a cold start that is at once: the map opens where the kept
// /v1/nearby answer was fetched for and shows it, until the device has a
// position (task 6.1).
export default function HomeScreen() {
  useDeviceLocation();
  const restoring = useIsRestoring();
  const queryClient = useQueryClient();
  const lastPlace = useMemo(
    () => (restoring ? null : lastNearbyCenter(queryClient)),
    [restoring, queryClient],
  );
  if (restoring) {
    return null; // reading the kept cache takes a few milliseconds: no spinner for that
  }
  return (
    <LocationGate lastPlace={lastPlace}>
      {({ position, source }) => (
        <MapScreen position={position} source={source} baseUrl={apiBaseUrl} client={apiClient} />
      )}
    </LocationGate>
  );
}
