import { describe, expect, it } from 'vitest';

import { distanceMeters } from './geo';

const wedding = { lat: 52.5427, lng: 13.366 };
const alexanderplatz = { lat: 52.521512, lng: 13.411267 };

describe('distanceMeters', () => {
  it('is 0 for the same point', () => {
    expect(distanceMeters(wedding, wedding)).toBe(0);
  });

  it('measures short distances (150 m is the refetch threshold)', () => {
    // 0.001° latitude ≈ 111.2 m everywhere.
    expect(distanceMeters(wedding, { lat: wedding.lat + 0.001, lng: wedding.lng })).toBeCloseTo(
      111.19,
      1,
    );
    // 0.001° longitude ≈ 111.2 m × cos(52.54°) ≈ 67.6 m in Berlin.
    expect(distanceMeters(wedding, { lat: wedding.lat, lng: wedding.lng + 0.001 })).toBeCloseTo(
      67.65,
      1,
    );
  });

  it('measures across the city and is symmetric', () => {
    // 2.36 km south, 3.06 km east.
    const d = distanceMeters(wedding, alexanderplatz);
    expect(d).toBeGreaterThan(3_850);
    expect(d).toBeLessThan(3_880);
    expect(distanceMeters(alexanderplatz, wedding)).toBeCloseTo(d, 6);
  });

  it('handles antipodes', () => {
    expect(distanceMeters({ lat: 0, lng: 0 }, { lat: 0, lng: 180 })).toBeCloseTo(
      Math.PI * 6_371_008.8,
      0,
    );
  });
});
