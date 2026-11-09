import { apiBaseUrl, apiClient } from '@/lib/api';
import { useDeviceLocation } from '@/lib/location';
import { LocationGate } from '@/ui/LocationGate';
import { MapScreen } from '@/ui/MapScreen';

// Start screen: the map once there is a position (device or map centre).
export default function HomeScreen() {
  useDeviceLocation();
  return (
    <LocationGate>
      {({ position, source }) => (
        <MapScreen position={position} source={source} baseUrl={apiBaseUrl} client={apiClient} />
      )}
    </LocationGate>
  );
}
