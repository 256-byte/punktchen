import { describe, expect, it } from 'vitest';

import { parseApiBaseUrl } from './apiBaseUrl';

describe('parseApiBaseUrl', () => {
  it.each([
    ['http://192.168.178.29:8080', 'http://192.168.178.29:8080'],
    ['http://192.168.178.29:8080/', 'http://192.168.178.29:8080'],
    ['  http://10.0.2.2:8080  ', 'http://10.0.2.2:8080'],
    ['https://api.example.com', 'https://api.example.com'],
    ['https://example.com/livestop//', 'https://example.com/livestop'],
    ['http://localhost:8080', 'http://localhost:8080'],
    ['http://[::1]:8080', 'http://[::1]:8080'],
  ])('accepts %s', (raw, url) => {
    expect(parseApiBaseUrl(raw)).toEqual({ ok: true, url });
  });

  it.each([undefined, '', '   '])('reports a missing value (%j)', (raw) => {
    expect(parseApiBaseUrl(raw)).toEqual({ ok: false, error: 'EXPO_PUBLIC_API_URL is not set' });
  });

  it.each([
    '192.168.178.29:8080', // no scheme
    'ftp://example.com',
    'http://',
    'http://example.com:port',
    'http://example.com/v1?x=1',
    'http://example.com/#top',
    'http://user:secret@example.com',
    'http://exa mple.com',
  ])('rejects %s', (raw) => {
    const result = parseApiBaseUrl(raw);
    expect(result.ok).toBe(false);
  });

  it.each(['http://example.com:0', 'http://example.com:65536'])('rejects port in %s', (raw) => {
    expect(parseApiBaseUrl(raw)).toMatchObject({
      ok: false,
      error: expect.stringContaining('port'),
    });
  });
});
