// Keeps the query cache in AsyncStorage (task 6.1). What is kept is decided
// in src/api/persistence.ts.
import AsyncStorage from '@react-native-async-storage/async-storage';
import { createAsyncStoragePersister } from '@tanstack/query-async-storage-persister';

import { PERSIST_KEY, serializeClient } from '@/api/persistence';

/** Writes at most once a second (the library's default throttle). */
export const queryPersister = createAsyncStoragePersister({
  storage: AsyncStorage,
  key: PERSIST_KEY,
  serialize: serializeClient,
});
