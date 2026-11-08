import { describe, expect, it, jest } from '@jest/globals';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { createApiClient, type ApiClientOptions } from '@/api/client';
import { BackendStatus } from '@/ui/BackendStatus';

const BASE = 'http://192.168.178.29:8080';

type FakeFetch = NonNullable<ApiClientOptions['fetch']>;

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

async function renderStatus(fakeFetch: FakeFetch, options: ApiClientOptions = {}) {
  const client = createApiClient(BASE, { fetch: fakeFetch, ...options });
  const queryClient = new QueryClient({ defaultOptions: { queries: { gcTime: Infinity } } });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
  await render(<BackendStatus baseUrl={{ ok: true, url: BASE }} client={client} />, { wrapper });
}

describe('BackendStatus', () => {
  it('shows the health result and calls /v1/health', async () => {
    const fakeFetch = jest.fn<FakeFetch>(async () =>
      json({ status: 'ok', version: 'dev', time: '2026-11-08T22:13:20+01:00' }),
    );
    await renderStatus(fakeFetch);

    expect(await screen.findByText('Backend: ok')).toBeOnTheScreen();
    expect(screen.getByText('version dev')).toBeOnTheScreen();
    expect(screen.getByText('server time 2026-11-08T22:13:20+01:00')).toBeOnTheScreen();
    expect(fakeFetch).toHaveBeenCalledTimes(1);
    expect(fakeFetch.mock.calls[0]?.[0].url).toBe(`${BASE}/v1/health`);
  });

  it('shows "down" when the backend reports it', async () => {
    await renderStatus(async () =>
      json({ status: 'down', version: 'dev', time: '2026-11-08T22:13:20+01:00' }),
    );
    expect(await screen.findByText('Backend: down')).toBeOnTheScreen();
  });

  it('shows a hint when the backend is unreachable', async () => {
    await renderStatus(async () => {
      throw new TypeError('Network request failed');
    });
    expect(await screen.findByText('Backend unreachable')).toBeOnTheScreen();
    expect(screen.getByText('Network request failed')).toBeOnTheScreen();
    expect(screen.getByText(/same Wi-Fi/)).toBeOnTheScreen();
  });

  it('gives up after the timeout', async () => {
    const hanging: FakeFetch = (_request, init) =>
      new Promise((_resolve, reject) => {
        init?.signal?.addEventListener('abort', () => reject(new Error('Aborted')));
      });
    await renderStatus(hanging, { timeoutMs: 50 });
    expect(await screen.findByText('no answer within 0.05 s')).toBeOnTheScreen();
  });

  it('checks again on button press', async () => {
    let calls = 0;
    await renderStatus(async () => {
      calls += 1;
      if (calls === 1) throw new TypeError('Network request failed');
      return json({ status: 'ok', version: 'dev', time: '2026-11-08T22:13:20+01:00' });
    });
    await screen.findByText('Backend unreachable');

    fireEvent.press(screen.getByRole('button', { name: 'Check again' }));
    expect(await screen.findByText('Backend: ok')).toBeOnTheScreen();
    expect(calls).toBe(2);
  });

  it('explains a missing configuration without calling the network', async () => {
    await render(
      <BackendStatus
        baseUrl={{ ok: false, error: 'EXPO_PUBLIC_API_URL is not set' }}
        client={null}
      />,
    );
    expect(screen.getByText('Backend not configured')).toBeOnTheScreen();
    expect(screen.getByText('EXPO_PUBLIC_API_URL is not set')).toBeOnTheScreen();
  });
});
