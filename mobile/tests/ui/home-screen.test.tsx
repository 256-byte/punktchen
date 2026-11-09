import { describe, expect, it, jest } from '@jest/globals';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react-native';

import HomeScreen from '../../app/index';
import { fake, position } from './location-mock';
import { map } from './maps-mock';

jest.mock(
  'expo-location',
  () => jest.requireActual<typeof import('./location-mock')>('./location-mock').expoLocationMock,
);
jest.mock(
  'react-native-maps',
  () => jest.requireActual<typeof import('./maps-mock')>('./maps-mock').mapsMock,
);

describe('HomeScreen', () => {
  it('opens the map at the device position', async () => {
    fake.current = { status: 'granted', canAskAgain: true };
    fake.lastKnown = position(52.5427, 13.366, Date.now());
    await render(
      <QueryClientProvider client={new QueryClient()}>
        <HomeScreen />
      </QueryClientProvider>,
    );
    expect(await screen.findByTestId('map')).toBeOnTheScreen();
    expect(map.props?.initialRegion).toMatchObject({ latitude: 52.5427, longitude: 13.366 });
  });
});
