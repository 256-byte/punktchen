import { describe, expect, it } from 'vitest';

import {
  clockOffset,
  countdownLabel,
  MINUTE,
  minutesLeft,
  parseTimestamp,
  SECOND,
  wallClock,
} from './time';

const at = (iso: string) => Date.parse(iso);

describe('parseTimestamp', () => {
  it.each([
    ['2026-11-05T14:32:00+01:00', '2026-11-05T13:32:00Z'],
    ['2026-07-01T09:05:30+02:00', '2026-07-01T07:05:30Z'],
    ['2026-11-05T13:32:00Z', '2026-11-05T13:32:00Z'],
    ['2026-11-05T14:32:00.250+01:00', '2026-11-05T13:32:00.250Z'],
  ])('parses %s', (value, utc) => {
    expect(parseTimestamp(value)).toBe(at(utc));
  });

  it.each([
    '',
    '2026-11-05',
    '2026-11-05T14:32+01:00', // no seconds
    '2026-11-05T14:32:00', // no offset: would be read as device time
    '2026-11-05 14:32:00+01:00',
    '2026-13-05T14:32:00+01:00', // month 13
    'tomorrow',
  ])('rejects %j', (value) => {
    expect(parseTimestamp(value)).toBeNull();
  });
});

describe('wallClock', () => {
  it('takes the time as written, in the offset of the timestamp', () => {
    expect(wallClock('2026-11-05T14:32:00+01:00')).toBe('14:32');
    expect(wallClock('2026-11-06T00:07:59+01:00')).toBe('00:07');
  });

  it('rejects invalid timestamps', () => {
    expect(wallClock('14:32')).toBeNull();
  });
});

describe('minutesLeft', () => {
  const dep = at('2026-11-05T14:32:00+01:00');

  it.each([
    [0, 0],
    [59 * SECOND, 0],
    [60 * SECOND, 1],
    [119 * SECOND, 1],
    [10 * MINUTE, 10],
    [-1 * SECOND, -1],
    [-61 * SECOND, -2],
  ])('%i ms before the departure → %i', (before, minutes) => {
    expect(minutesLeft(dep, dep - before)).toBe(minutes);
  });
});

describe('countdownLabel', () => {
  const dep = at('2026-11-05T14:32:00+01:00');

  it.each([
    [-30 * SECOND, 'now'], // left during its minute: still "now"
    [0, 'now'],
    [59 * SECOND, 'now'],
    [60 * SECOND, '1 min'],
    [3 * MINUTE + 20 * SECOND, '3 min'],
    [59 * MINUTE + 59 * SECOND, '59 min'],
    [60 * MINUTE, '14:32'],
    [85 * MINUTE, '14:32'],
  ])('%i ms before → %s', (before, label) => {
    expect(countdownLabel(dep, '14:32', dep - before)).toBe(label);
  });
});

describe('clockOffset', () => {
  const server = '2026-11-05T14:30:00+01:00';
  const serverMs = at(server);

  it('ignores small differences (latency, a slightly wrong clock)', () => {
    expect(clockOffset(server, serverMs)).toBe(0);
    expect(clockOffset(server, serverMs + 30 * SECOND)).toBe(0);
    expect(clockOffset(server, serverMs - 30 * SECOND)).toBe(0);
  });

  it('corrects a device clock that is more than 30 s off', () => {
    expect(clockOffset(server, serverMs + 2 * MINUTE)).toBe(-2 * MINUTE); // device ahead
    expect(clockOffset(server, serverMs - 31 * SECOND)).toBe(31 * SECOND); // device behind
  });

  it('ignores an invalid server time', () => {
    expect(clockOffset('soon', serverMs + 10 * MINUTE)).toBe(0);
  });
});
