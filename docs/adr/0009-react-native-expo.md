# 0009 — React Native + Expo for mobile

- **Status:** accepted
- **Date:** 2026-11-05

## Context
Developer works on Windows with an iPhone 15; no Mac. Wants both iOS and Android eventually and prefers React.

## Decision
React Native + Expo (latest SDK), TypeScript strict, Expo Router. Daily development via Expo Go on the iPhone and the Android emulator. Maps via `react-native-maps` (Apple Maps on iOS, Google Maps on Android).

## Alternatives considered
- SwiftUI (native): best iOS feel, but no Xcode on Windows; xtool works but no simulator/previews, licensing grey area.
- Flutter: strong UI toolkit, but cannot run on the iPhone from Windows without a Mac or cloud builds.

## Consequences
Fast edit-reload loop on a real iPhone from Windows. Custom native modules (e.g. widgets via `expo-widgets`) need a development build (EAS cloud for iOS) — deferred to later phases.
