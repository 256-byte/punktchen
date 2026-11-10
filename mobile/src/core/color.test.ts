import { describe, expect, it } from 'vitest';

import { contrastRatio, relativeLuminance } from './color';

describe('relativeLuminance', () => {
  it('is 0 for black and 1 for white', () => {
    expect(relativeLuminance('#000000')).toBe(0);
    expect(relativeLuminance('#FFFFFF')).toBe(1);
    expect(relativeLuminance('#ffffff')).toBe(1);
  });

  it('weights green most', () => {
    expect(relativeLuminance('#00FF00')).toBeCloseTo(0.7152, 4);
    expect(relativeLuminance('#FF0000')).toBeCloseTo(0.2126, 4);
  });

  it('is null for anything but #RRGGBB', () => {
    expect(relativeLuminance('#fff')).toBeNull();
    expect(relativeLuminance('rgba(0,0,0,0.5)')).toBeNull();
    expect(relativeLuminance('red')).toBeNull();
  });
});

describe('contrastRatio', () => {
  it('is 21 for black on white, either way round', () => {
    expect(contrastRatio('#000000', '#FFFFFF')).toBeCloseTo(21, 5);
    expect(contrastRatio('#FFFFFF', '#000000')).toBeCloseTo(21, 5);
  });

  it('is 1 for the same colour', () => {
    expect(contrastRatio('#767676', '#767676')).toBe(1);
  });

  it('matches known values', () => {
    // #767676 on white is the classic "just AA" grey.
    expect(contrastRatio('#767676', '#FFFFFF')).toBeCloseTo(4.54, 2);
  });

  it('throws for colours it cannot read', () => {
    expect(() => contrastRatio('#fff', '#000000')).toThrow('#fff');
    expect(() => contrastRatio('#000000', 'blue')).toThrow('blue');
  });
});
