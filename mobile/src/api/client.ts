// Typed HTTP client for our backend (api/openapi.yaml). The app talks only to
// this backend, never to VBB directly (CLAUDE.md rule 6).
import createClient from 'openapi-fetch';

import type { components, paths } from './schema.gen';

export type Health = components['schemas']['Health'];
export type ApiClient = ReturnType<typeof createClient<paths>>;

/** Without a timeout, an unreachable LAN address hangs for about a minute on iOS. */
export const DEFAULT_TIMEOUT_MS = 5000;

export type ApiClientOptions = {
  timeoutMs?: number;
  /** Replaces global fetch (tests). */
  fetch?: (request: Request, init?: RequestInit) => Promise<Response>;
};

export function createApiClient(baseUrl: string, options: ApiClientOptions = {}): ApiClient {
  const timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS;
  const baseFetch =
    options.fetch ?? ((request: Request, init?: RequestInit) => fetch(request, init));
  return createClient<paths>({
    baseUrl,
    fetch: (request) => fetchWithTimeout(baseFetch, request, timeoutMs),
  });
}

/** Thrown when the backend cannot be reached or answers with an error. */
export class ApiError extends Error {
  constructor(
    message: string,
    readonly status?: number,
  ) {
    super(message);
    this.name = 'ApiError';
  }
}

export async function fetchHealth(client: ApiClient, signal?: AbortSignal): Promise<Health> {
  const { data, error, response } = await client.GET('/v1/health', { signal });
  if (data) {
    return data;
  }
  throw new ApiError(error?.error.message ?? `HTTP ${response.status}`, response.status);
}

// Aborts after timeoutMs, and also when the caller's signal aborts (e.g.
// TanStack Query cancelling a request).
async function fetchWithTimeout(
  baseFetch: NonNullable<ApiClientOptions['fetch']>,
  request: Request,
  timeoutMs: number,
): Promise<Response> {
  const controller = new AbortController();
  let timedOut = false;
  const timer = setTimeout(() => {
    timedOut = true;
    controller.abort();
  }, timeoutMs);
  const onCallerAbort = () => controller.abort();
  request.signal?.addEventListener('abort', onCallerAbort);

  try {
    return await baseFetch(request, { signal: controller.signal });
  } catch (err) {
    if (timedOut) {
      throw new ApiError(`no answer within ${timeoutMs / 1000} s`);
    }
    if (request.signal?.aborted) {
      throw err; // cancelled by the caller: not an API failure
    }
    const message = err instanceof Error && err.message ? err.message : 'network request failed';
    throw new ApiError(message);
  } finally {
    clearTimeout(timer);
    request.signal?.removeEventListener('abort', onCallerAbort);
  }
}
