// Runs before every Jest test file (package.json "jest.setupFiles").
import { jest } from '@jest/globals';

// First render in a worker loads React Native + navigation trees under parallel load.
jest.setTimeout(15_000);

// Safe area insets (attribution on the map): the library's own Jest mock.
jest.mock(
  'react-native-safe-area-context',
  () => jest.requireActual<{ default: object }>('react-native-safe-area-context/jest/mock').default,
);

// The bottom sheet needs Reanimated's native side, which Jest does not have.
jest.mock(
  '@gorhom/bottom-sheet',
  () =>
    jest.requireActual<typeof import('./bottom-sheet-mock')>('./bottom-sheet-mock').bottomSheetMock,
);

// AsyncStorage (persisted query cache): the library's in-memory Jest mock.
jest.mock('@react-native-async-storage/async-storage', () =>
  jest.requireActual('@react-native-async-storage/async-storage/jest/async-storage-mock'),
);
