// ETag revalidation shared by the GET endpoints (DATA_MODEL §5.4): the
// previous answer's ETag goes out as If-None-Match; a 304 keeps the previous
// data and only moves its "last checked" time.
import { clockOffset } from '@/core/time';

import { ApiError } from './client';

/** One answer as the app keeps it. */
export interface Kept<T> {
  response: T;
  /** ETag of the response; null if the server sent none. */
  etag: string | null;
  /** Device time of the last 200 or 304 (data age, ARCHITECTURE §4.3). */
  checkedAt: number;
  /** Add to the device clock to get server time (ADR-0007); from the last 200. */
  clockOffsetMs: number;
}

export interface RevalidateOptions<T> {
  /** The data kept for this request, if any: its ETag is revalidated. */
  previous?: Kept<T>;
  signal?: AbortSignal;
  /** Device clock (tests). */
  now?: () => number;
}

/** The If-None-Match header for the kept answer (none without an ETag). */
export function conditionalHeader<T>(previous: Kept<T> | undefined) {
  const etag = previous?.etag ?? null;
  return etag === null ? {} : { 'If-None-Match': etag };
}

/**
 * Turns an openapi-fetch result into the kept answer: 200 → new data, ETag
 * and clock offset; 304 → the previous data with a new checkedAt (same
 * response object, so nothing that reads it re-renders); else ApiError with
 * the server's message.
 */
export function keepAnswer<T extends { meta: { generatedAt: string } }>(
  result: { data?: T; error?: unknown; response: Response },
  previous: Kept<T> | undefined,
  checkedAt: number,
): Kept<T> {
  const { data, error, response } = result;
  const status = response.status;
  if (status === 304) {
    if (previous === undefined) {
      throw new ApiError('304 Not Modified without a previous response', 304);
    }
    return { ...previous, etag: response.headers.get('ETag') ?? previous.etag, checkedAt };
  }
  if (data) {
    return {
      response: data,
      etag: response.headers.get('ETag'),
      checkedAt,
      clockOffsetMs: clockOffset(data.meta.generatedAt, checkedAt),
    };
  }
  throw new ApiError(errorMessage(error) ?? `HTTP ${status}`, status);
}

function errorMessage(error: unknown): string | undefined {
  if (typeof error === 'object' && error !== null && 'error' in error) {
    const inner = (error as { error?: { message?: unknown } }).error;
    if (typeof inner?.message === 'string') return inner.message;
  }
  return undefined;
}
