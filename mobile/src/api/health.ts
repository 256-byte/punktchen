import { useQuery } from '@tanstack/react-query';

import { fetchHealth, type ApiClient } from './client';

export function useHealth(client: ApiClient) {
  return useQuery({
    queryKey: ['health'],
    queryFn: ({ signal }) => fetchHealth(client, signal),
    retry: false,
  });
}
