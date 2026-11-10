import { describe, expect, it } from 'vitest';

import { freshness, statusBanner, updatedAgoLabel } from './staleness';
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

describe('statusBanner', () => {
  const at = (age: number) => checkedAt + age;

  it('says nothing while the data is fresh and requests work', () => {
    expect(statusBanner(checkedAt, at(59 * SECOND), null)).toBeNull();
  });

  it('shows the age from 60 s', () => {
    expect(statusBanner(checkedAt, at(2 * MINUTE), null)).toEqual({
      title: 'Updated 2 min ago',
      detail: null,
      warning: false,
    });
  });

  it('warns about live times after 5 min', () => {
    expect(statusBanner(checkedAt, at(7 * MINUTE), null)).toEqual({
      title: 'Updated 7 min ago',
      detail: 'Live times may be out of date',
      warning: true,
    });
  });

  it('says at once when the backend cannot be reached', () => {
    expect(statusBanner(checkedAt, at(10 * SECOND), 'unreachable')).toEqual({
      title: 'No connection — showing last known data',
      detail: 'updated just now',
      warning: true,
    });
    expect(statusBanner(checkedAt, at(14 * 60 * MINUTE), 'unreachable')).toEqual({
      title: 'No connection — showing last known data',
      detail: 'updated 14 h ago, live times may be out of date',
      warning: true,
    });
  });

  it('tells a server error from no connection', () => {
    expect(statusBanner(checkedAt, at(3 * MINUTE), 'server')).toEqual({
      title: 'Server problem — showing last known data',
      detail: 'updated 3 min ago',
      warning: true,
    });
  });
});
