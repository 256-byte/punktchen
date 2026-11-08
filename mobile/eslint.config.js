// ESLint flat config: Expo's rules, Prettier owns formatting, and the
// src/core boundary from CLAUDE.md rule 10.
const { defineConfig } = require('eslint/config');
const expoConfig = require('eslint-config-expo/flat');
const prettierConfig = require('eslint-config-prettier/flat');

// src/core is pure TypeScript domain logic, testable with Vitest in Node.
// It must not depend on React, React Native or Expo (ARCHITECTURE §4).
const coreForbiddenImports = {
  patterns: [
    {
      regex: '^(react|react-native|expo)(/.*)?$',
      message: 'src/core is pure TypeScript: no React, React Native or Expo imports.',
    },
    {
      regex: '^(react-native-|expo-|@expo/|@react-native/|@react-navigation/)',
      message: 'src/core is pure TypeScript: no React Native or Expo packages.',
    },
  ],
};

module.exports = defineConfig([
  expoConfig,
  prettierConfig,
  {
    ignores: ['node_modules/', '.expo/', 'dist/', 'coverage/', 'src/api/*.gen.ts', 'expo-env.d.ts'],
  },
  {
    files: ['src/core/**/*.{ts,tsx}'],
    rules: {
      'no-restricted-imports': ['error', coreForbiddenImports],
    },
  },
]);
