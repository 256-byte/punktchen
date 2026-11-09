import { afterEach, describe, expect, it, jest } from '@jest/globals';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { ApiError, createApiClient, type ApiClientOptions } from '@/api/client';
import { fetchNearby, type NearbyParams, type NearbyResponse } from '@/api/nearby';
import { nearbyQueryKey, useNearby } from '@/api/useNearby';

const BASE = 'http://192.168.178.29:8080';
const WEDDING: NearbyParams = { lat: 52.5427, lng: 13.366 };

type FakeFetch = NonNullable<ApiClientOptions['fetch']>;

function body(generatedAt = '2026-11-05T14:30:12+01:00'): NearbyResponse {
  return {
    meta: {
      generatedAt,
      attribution: ['Data: VBB (CC BY 4.0)'],
      layers: { transit: { version: 18342, status: 'ok' } },
    },
    layers: { transit: { layer: 'transit', features: [] } },
  };
}

function ok(b: NearbyResponse, etag?: string): Response {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  if (etag) headers.ETag = etag;
  return new Response(JSON.stringify(b), { status: 200, headers });
}

function notModified(etag: string): Response {
  return new Response(null, { status: 304, headers: { ETag: etag } });
}

/** Answers in order; records each request. */
function fakeServer(...answers: (() => Response | Promise<Response>)[]) {
  const requests: Request[] = [];
  const fetch = jest.fn<FakeFetch>(async (request) => {
    requests.push(request);
    const answer = answers[requests.length - 1];
    if (!answer) throw new Error(`unexpected request ${requests.length}: ${request.url}`);
    return answer();
  });
  return { client: createApiClient(BASE, { fetch }), requests };
}

function wrapper() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { gcTime: Infinity } } });
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

afterEach(() => {
  jest.restoreAllMocks();
});

describe('fetchNearby', () => {
  it('sends the parameters as in the contract', async () => {
    const server = fakeServer(() => ok(body(), '"v1"'));
    await fetchNearby(server.client, {
      ...WEDDING,
      radius: 800,
      layers: ['transit', 'toilets' as 'transit'],
    });
    const url = new URL(server.requests[0]!.url);
    expect(url.pathname).toBe('/v1/nearby');
    expect(url.searchParams.get('lat')).toBe('52.5427');
    expect(url.searchParams.get('lng')).toBe('13.366');
    expect(url.searchParams.get('radius')).toBe('800');
    expect(url.searchParams.getAll('layers')).toEqual(['transit,toilets']); // explode: false
    expect(server.requests[0]!.headers.get('If-None-Match')).toBeNull();
  });

  it('keeps ETag, check time and clock offset of a 200', async () => {
    const server = fakeServer(() => ok(body('2026-11-05T14:30:00+01:00'), '"v1"'));
    const deviceTime = Date.parse('2026-11-05T14:32:00+01:00'); // 2 min ahead
    const got = await fetchNearby(server.client, WEDDING, { now: () => deviceTime });
    expect(got).toEqual({
      response: body('2026-11-05T14:30:00+01:00'),
      etag: '"v1"',
      checkedAt: deviceTime,
      clockOffsetMs: -120_000,
    });
  });

  it('has no ETag when the server sends none', async () => {
    const server = fakeServer(() => ok(body()));
    expect((await fetchNearby(server.client, WEDDING)).etag).toBeNull();
  });

  it('revalidates with If-None-Match and keeps the data on 304', async () => {
    const server = fakeServer(() => notModified('"v1"'));
    const previous = { response: body(), etag: '"v1"', checkedAt: 1000, clockOffsetMs: 5 };
    const got = await fetchNearby(server.client, WEDDING, { previous, now: () => 31_000 });

    expect(server.requests[0]!.headers.get('If-None-Match')).toBe('"v1"');
    expect(got.response).toBe(previous.response); // same object: nothing re-renders for it
    expect(got).toEqual({ ...previous, checkedAt: 31_000 });
  });

  it('rejects a 304 it did not ask for', async () => {
    const server = fakeServer(() => notModified('"v1"'));
    await expect(fetchNearby(server.client, WEDDING)).rejects.toEqual(
      new ApiError('304 Not Modified without a previous response', 304),
    );
  });

  it('reports the server error message', async () => {
    const server = fakeServer(
      () =>
        new Response(
          JSON.stringify({
            error: { code: 'invalid_argument', message: 'radius must be between 100 and 1000' },
          }),
          { status: 400, headers: { 'Content-Type': 'application/json' } },
        ),
      () => new Response('bad gateway', { status: 502 }),
    );
    await expect(fetchNearby(server.client, { ...WEDDING, radius: 5000 })).rejects.toMatchObject({
      message: 'radius must be between 100 and 1000',
      status: 400,
    });
    await expect(fetchNearby(server.client, WEDDING)).rejects.toMatchObject({
      message: 'HTTP 502',
      status: 502,
    });
  });
});

describe('nearbyQueryKey', () => {
  it('fills in the defaults, so equal requests share a cache entry', () => {
    expect(nearbyQueryKey(WEDDING)).toEqual(
      nearbyQueryKey({ ...WEDDING, radius: 500, layers: ['transit'] }),
    );
    expect(nearbyQueryKey(WEDDING)).not.toEqual(nearbyQueryKey({ ...WEDDING, radius: 600 }));
  });
});

describe('useNearby', () => {
  it('keeps the previous data when a refetch gets 304', async () => {
    const server = fakeServer(
      () => ok(body(), '"v1"'),
      () => notModified('"v1"'),
    );
    const now = jest.spyOn(Date, 'now').mockReturnValue(1_000);
    const { result } = await renderHook(() => useNearby(server.client, WEDDING), {
      wrapper: wrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    const first = result.current.data!;
    expect(first.etag).toBe('"v1"');
    expect(first.checkedAt).toBe(1_000);

    now.mockReturnValue(31_000);
    await act(async () => {
      await result.current.refetch();
    });

    expect(server.requests).toHaveLength(2);
    expect(server.requests[1]!.headers.get('If-None-Match')).toBe('"v1"');
    await waitFor(() => expect(result.current.data!.checkedAt).toBe(31_000));
    expect(result.current.isSuccess).toBe(true);
    expect(result.current.data!.response).toBe(first.response);
    expect(result.current.data!.etag).toBe('"v1"');
  });

  it('replaces the data and ETag on a 200', async () => {
    const server = fakeServer(
      () => ok(body('2026-11-05T14:30:12+01:00'), '"v1"'),
      () => ok(body('2026-11-05T14:31:12+01:00'), '"v2"'),
    );
    const { result } = await renderHook(() => useNearby(server.client, WEDDING), {
      wrapper: wrapper(),
    });
    // Read data before the refetch: TanStack Query re-renders only for
    // result fields that were read (tracked properties).
    await waitFor(() => expect(result.current.data?.etag).toBe('"v1"'));
    await act(async () => {
      await result.current.refetch();
    });
    await waitFor(() => expect(result.current.data!.etag).toBe('"v2"'));
    expect(result.current.data!.response.meta.generatedAt).toBe('2026-11-05T14:31:12+01:00');
  });

  it('shows the previous area while new parameters load, without its ETag', async () => {
    // The second answer waits until the placeholder was seen.
    let release: () => void = () => {};
    const gate = new Promise<void>((resolve) => (release = resolve));
    const server = fakeServer(
      () => ok(body(), '"wedding"'),
      async () => {
        await gate;
        return ok(body('2026-11-05T14:30:40+01:00'), '"alex"');
      },
    );
    const { result, rerender } = await renderHook(
      (params: NearbyParams) => useNearby(server.client, params),
      { wrapper: wrapper(), initialProps: WEDDING },
    );
    await waitFor(() => expect(result.current.data?.etag).toBe('"wedding"'));

    await rerender({ lat: 52.5215, lng: 13.4113 });
    expect(result.current.isPlaceholderData).toBe(true);
    expect(result.current.data!.etag).toBe('"wedding"');

    release();
    await waitFor(() => expect(result.current.isPlaceholderData).toBe(false));
    expect(result.current.data!.etag).toBe('"alex"');
    expect(server.requests[1]!.headers.get('If-None-Match')).toBeNull();
  });

  it('does not fetch without parameters', async () => {
    const server = fakeServer();
    const { result } = await renderHook(() => useNearby(server.client, null), {
      wrapper: wrapper(),
    });
    expect(result.current.fetchStatus).toBe('idle');
    expect(result.current.data).toBeUndefined();
    expect(server.requests).toHaveLength(0);
  });

  it('reports errors without retrying', async () => {
    const server = fakeServer(() => new Response('', { status: 500 }));
    const { result } = await renderHook(() => useNearby(server.client, WEDDING), {
      wrapper: wrapper(),
    });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.error).toMatchObject({ status: 500 });
    expect(server.requests).toHaveLength(1);
  });
});
