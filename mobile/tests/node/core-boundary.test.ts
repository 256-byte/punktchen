import { ESLint } from 'eslint';
import { describe, expect, it } from 'vitest';

// Guards CLAUDE.md rule 10: src/core must not import React, React Native or
// Expo. Runs the real ESLint config on in-memory files.
const eslint = new ESLint({ cwd: process.cwd() });

async function restrictedImports(filePath: string, source: string): Promise<string[]> {
  const [result] = await eslint.lintText(`${source}\n`, { filePath });
  return (result?.messages ?? [])
    .filter((m) => m.ruleId === 'no-restricted-imports')
    .map((m) => m.message);
}

describe('src/core import boundary', () => {
  it.each([
    "import { useState } from 'react';",
    "import { jsx } from 'react/jsx-runtime';",
    "import { View } from 'react-native';",
    "import Constants from 'expo-constants';",
    "import * as Location from 'expo-location';",
    "import { registerRootComponent } from 'expo';",
    "import MapView from 'react-native-maps';",
    "import { Ionicons } from '@expo/vector-icons';",
    "export { Platform } from 'react-native';",
  ])('rejects %s', async (source) => {
    expect(await restrictedImports('src/core/example.ts', source)).toHaveLength(1);
  });

  it.each([
    "import { other } from './other';",
    "import type { components } from '@/api/schema.gen';",
    "import { reactive } from './react-helpers';",
  ])('allows %s', async (source) => {
    expect(await restrictedImports('src/core/example.ts', source)).toEqual([]);
  });

  it('does not apply outside src/core', async () => {
    expect(
      await restrictedImports('src/ui/example.tsx', "import { View } from 'react-native';"),
    ).toEqual([]);
  });
});
