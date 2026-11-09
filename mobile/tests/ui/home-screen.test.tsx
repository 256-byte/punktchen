import { describe, expect, it, jest } from '@jest/globals';
import { render, screen } from '@testing-library/react-native';

import HomeScreen from '../../app/index';
import { fake, position } from './location-mock';

jest.mock(
  'expo-location',
  () => jest.requireActual<typeof import('./location-mock')>('./location-mock').expoLocationMock,
);

describe('HomeScreen', () => {
  it('shows the app name and where it looks', async () => {
    fake.current = { status: 'granted', canAskAgain: true };
    fake.lastKnown = position(52.5427, 13.366, Date.now());
    await render(<HomeScreen />);
    expect(await screen.findByText('LiveStop')).toBeOnTheScreen();
    expect(
      screen.getByText('Looking around your last known location: 52.54270, 13.36600'),
    ).toBeOnTheScreen();
  });
});
