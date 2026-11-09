import { describe, expect, it } from 'vitest';

import { acceptFix, BERLIN_CENTER, type Fix, locationView, type Permission } from './location';

const t0 = Date.parse('2026-11-05T14:30:00+01:00');

function fix(patch: Partial<Fix> = {}): Fix {
  return { lat: 52.5427, lng: 13.366, accuracy: 20, at: t0, source: 'live', ...patch };
}

// 0.0001° latitude ≈ 11.1 m.
const north = (m: number) => 52.5427 + (m / 11.12) * 0.0001;

describe('acceptFix', () => {
  it('takes the first fix', () => {
    expect(acceptFix(null, fix({ source: 'lastKnown', accuracy: null }))).toBe(true);
  });

  it('replaces a cached fix with the first live one, even nearby', () => {
    expect(acceptFix(fix({ source: 'lastKnown' }), fix({ at: t0 + 1000 }))).toBe(true);
  });

  it('ignores jitter under 25 m', () => {
    expect(acceptFix(fix(), fix({ lat: north(10), at: t0 + 5000 }))).toBe(false);
    expect(acceptFix(fix(), fix({ lat: north(24), at: t0 + 5000 }))).toBe(false);
  });

  it('takes a move of 25 m or more', () => {
    expect(acceptFix(fix(), fix({ lat: north(26), at: t0 + 5000 }))).toBe(true);
  });

  it('takes a fix at least twice as precise', () => {
    const coarse = fix({ accuracy: 100 });
    expect(acceptFix(coarse, fix({ accuracy: 50, at: t0 + 1000 }))).toBe(true);
    expect(acceptFix(coarse, fix({ accuracy: 60, at: t0 + 1000 }))).toBe(false);
    expect(acceptFix(fix({ accuracy: null }), fix({ accuracy: 300, at: t0 + 1000 }))).toBe(true);
    expect(acceptFix(coarse, fix({ accuracy: null, at: t0 + 1000 }))).toBe(false);
  });

  it('ignores an older fix that arrives late', () => {
    expect(acceptFix(fix(), fix({ lat: north(500), at: t0 - 1000 }))).toBe(false);
  });
});

describe('locationView', () => {
  const granted: Permission = { status: 'granted', canAskAgain: true };
  const center = { lat: 52.5, lng: 13.4 };

  it('waits for the permission answer', () => {
    expect(locationView({ status: 'unknown', canAskAgain: true }, 'device', null, center)).toEqual({
      kind: 'checking',
    });
  });

  it('waits for the first fix once granted', () => {
    expect(locationView(granted, 'device', null, center)).toEqual({ kind: 'checking' });
  });

  it('uses the device position', () => {
    expect(locationView(granted, 'device', fix({ source: 'lastKnown' }), center)).toEqual({
      kind: 'ready',
      position: { lat: 52.5427, lng: 13.366 },
      source: 'lastKnown',
    });
  });

  it('explains a denied permission, and whether the prompt can come again', () => {
    expect(locationView({ status: 'denied', canAskAgain: true }, 'device', null, center)).toEqual({
      kind: 'denied',
      canAskAgain: true,
    });
    expect(locationView({ status: 'denied', canAskAgain: false }, 'device', null, center)).toEqual({
      kind: 'denied',
      canAskAgain: false,
    });
  });

  it('uses the map centre when the user chose it, whatever the permission', () => {
    for (const status of ['unknown', 'granted', 'denied'] as const) {
      expect(locationView({ status, canAskAgain: false }, 'mapCenter', fix(), center)).toEqual({
        kind: 'ready',
        position: center,
        source: 'mapCenter',
      });
    }
  });

  it('starts the map centre in Berlin Mitte', () => {
    expect(BERLIN_CENTER.lat).toBeCloseTo(52.52, 1);
    expect(BERLIN_CENTER.lng).toBeCloseTo(13.41, 1);
  });
});
