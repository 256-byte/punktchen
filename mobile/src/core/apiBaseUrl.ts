// Validation of the backend base URL (EXPO_PUBLIC_API_URL).
//
// Parsed with a regex instead of `new URL()`: React Native's URL polyfill
// does not implement all getters, and this module must behave the same in
// Node (Vitest) and on the device.

export type ApiBaseUrl = { ok: true; url: string } | { ok: false; error: string };

// scheme://host[:port][/path] — no query, no fragment, no credentials.
const BASE_URL = /^(https?):\/\/([A-Za-z0-9.-]+|\[[0-9A-Fa-f:.]+\])(?::(\d{1,5}))?(\/[^\s?#]*)?$/;

/**
 * Checks a configured API base URL and normalizes it (no trailing slash), so
 * paths like "/v1/health" can be appended directly.
 */
export function parseApiBaseUrl(raw: string | undefined): ApiBaseUrl {
  const value = raw?.trim() ?? '';
  if (value === '') {
    return { ok: false, error: 'EXPO_PUBLIC_API_URL is not set' };
  }

  const match = BASE_URL.exec(value);
  if (!match) {
    return {
      ok: false,
      error: `EXPO_PUBLIC_API_URL "${value}" is not a valid http(s) URL like http://192.168.1.20:8080`,
    };
  }

  const port = match[3];
  if (port !== undefined && (Number(port) < 1 || Number(port) > 65535)) {
    return { ok: false, error: `EXPO_PUBLIC_API_URL "${value}" has an invalid port` };
  }

  return { ok: true, url: value.replace(/\/+$/, '') };
}
