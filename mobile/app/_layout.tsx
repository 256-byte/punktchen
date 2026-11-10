import { QueryClient } from '@tanstack/react-query';
import { PersistQueryClientProvider } from '@tanstack/react-query-persist-client';
import { Stack } from 'expo-router';
import { useState } from 'react';
import { StyleSheet } from 'react-native';
import { GestureHandlerRootView } from 'react-native-gesture-handler';

import { persistOptions } from '@/api/persistence';
import { queryPersister } from '@/lib/queryPersister';

export default function RootLayout() {
  // The newest /v1/nearby answer is kept on the device and restored on
  // start (task 6.1).
  const [{ queryClient, options }] = useState(() => {
    const client = new QueryClient();
    return { queryClient: client, options: persistOptions(client, queryPersister) };
  });
  return (
    // Gestures (bottom sheet) need the root view around the whole app.
    <GestureHandlerRootView style={styles.root}>
      <PersistQueryClientProvider client={queryClient} persistOptions={options}>
        <Stack screenOptions={{ headerShown: false }} />
      </PersistQueryClientProvider>
    </GestureHandlerRootView>
  );
}

const styles = StyleSheet.create({
  root: {
    flex: 1,
  },
});
