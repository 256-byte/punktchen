import { describe, expect, it } from 'vitest';

import { freshness, updatedAgoLabel } from './staleness';
import { MINUTE, SECOND } from './time';

const checkedAt = Date.parse('2026-11-05T14:30:00+01:00');

describe('freshness', () => {
  it.each([
    [0, 'fresh'],
    [59 * SECOND, 'fresh'],
    [60 * SECOND, 'aging'],
    [5 * MINUTE, 'aging'],
    [5 * MINUTE + 1, 'stale'],
    [3 * 60 * MINUTE, 'stale'],
  ] as const)('%i ms old → %s', (age, want) => {
    expect(freshness(checkedAt, checkedAt + age)).toBe(want);
  });

  it('treats a check from the "future" (clock set back) as fresh', () => {
    expect(freshness(checkedAt, checkedAt - MINUTE)).toBe('fresh');
  });
});

describe('updatedAgoLabel', () => {
  it.each([
    [-5 * SECOND, 'updated just now'],
    [0, 'updated just now'],
    [59 * SECOND, 'updated just now'],
    [60 * SECOND, 'updated 1 min ago'],
    [4 * MINUTE + 30 * SECOND, 'updated 4 min ago'],
    [59 * MINUTE, 'updated 59 min ago'],
    [60 * MINUTE, 'updated 1 h ago'],
    [150 * MINUTE, 'updated 2 h ago'],
  ])('%i ms old → %s', (age, want) => {
    expect(updatedAgoLabel(checkedAt, checkedAt + age)).toBe(want);
  });
});
