// Runs before every Jest test file (package.json "jest.setupFiles").
import { jest } from '@jest/globals';

// The bottom sheet needs Reanimated's native side, which Jest does not have.
jest.mock(
  '@gorhom/bottom-sheet',
  () =>
    jest.requireActual<typeof import('./bottom-sheet-mock')>('./bottom-sheet-mock').bottomSheetMock,
);
