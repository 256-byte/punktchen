import { createApiClient, type ApiClient } from '@/api/client';
import { parseApiBaseUrl, type ApiBaseUrl } from '@/core/apiBaseUrl';

// Expo inlines EXPO_PUBLIC_* variables into the bundle when Metro builds it
// (from mobile/.env), so this must stay a literal `process.env.EXPO_PUBLIC_…`
// access. Changing .env needs a Metro restart.
export const apiBaseUrl: ApiBaseUrl = parseApiBaseUrl(process.env.EXPO_PUBLIC_API_URL);

export const apiClient: ApiClient | null = apiBaseUrl.ok ? createApiClient(apiBaseUrl.url) : null;
