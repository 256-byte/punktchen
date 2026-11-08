import { defineConfig } from 'vitest/config';

// Vitest runs the pure TypeScript code (src/core) and Node-only tooling
// tests in plain Node. Everything that renders React Native runs in Jest
// (jest-expo), see "jest" in package.json.
export default defineConfig({
  test: {
    environment: 'node',
    include: ['src/core/**/*.test.ts', 'tests/node/**/*.test.ts'],
  },
});
