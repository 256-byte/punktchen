import { describe, expect, it } from 'vitest';

import {
  cellDescription,
  type DepartureCell,
  departureCell,
  decodeCell,
  delayMinutes,
  delayText,
  encodeCell,
} from './realtime';

describe('delayMinutes', () => {
  it.each([
    [0, 0],
    [59, 0],
    [60, 1],
    [119, 1],
    [600, 10],
    [-30, 0],
    [-60, -1],
    [-119, -1],
  ])('%i s → %i min (toward zero)', (seconds, minutes) => {
    expect(Object.is(delayMinutes(seconds), minutes)).toBe(true); // no -0
  });
});

describe('delayText', () => {
  it('signs the minutes; nothing when on time', () => {
    expect(delayText(2)).toBe('+2');
    expect(delayText(-1)).toBe('−1');
    expect(delayText(0)).toBeNull();
  });
});

describe('departureCell', () => {
  const base = { label: '3 min', cancelled: false, source: 'realtime' as const };

  it('tells timetable, on time, early and late apart', () => {
    expect(departureCell({ ...base, source: 'scheduled', delaySeconds: null })).toEqual({
      label: '3 min',
      state: 'scheduled',
      delay: null,
    });
    expect(departureCell({ ...base, delaySeconds: 30 })).toEqual({
      label: '3 min',
      state: 'onTime',
      delay: null,
    });
    expect(departureCell({ ...base, delaySeconds: -60 })).toEqual({
      label: '3 min',
      state: 'early',
      delay: '−1',
    });
    expect(departureCell({ ...base, delaySeconds: 150 })).toEqual({
      label: '3 min',
      state: 'late',
      delay: '+2',
    });
  });

  it('marks cancelled ones whatever their realtime fields', () => {
    expect(departureCell({ ...base, cancelled: true, delaySeconds: 120 })).toEqual({
      label: '3 min',
      state: 'cancelled',
      delay: null,
    });
  });

  it('reads realtime without a delay as timetable', () => {
    expect(departureCell({ ...base, delaySeconds: null }).state).toBe('scheduled');
  });
});

describe('cellDescription', () => {
  it.each<[DepartureCell, string]>([
    [{ label: '14:52', state: 'scheduled', delay: null }, '14:52, timetable'],
    [{ label: 'now', state: 'onTime', delay: null }, 'now, live, on time'],
    [{ label: '3 min', state: 'late', delay: '+12' }, '3 min, live, 12 min late'],
    [{ label: '3 min', state: 'early', delay: '−1' }, '3 min, live, 1 min early'],
    [{ label: '3 min', state: 'cancelled', delay: null }, '3 min, cancelled'],
  ])('%o → %s', (cell, text) => {
    expect(cellDescription(cell)).toBe(text);
  });
});

describe('encodeCell / decodeCell', () => {
  it.each<DepartureCell>([
    { label: '14:52', state: 'scheduled', delay: null },
    { label: 'now', state: 'onTime', delay: null },
    { label: '3 min', state: 'late', delay: '+12' },
    { label: '3 min', state: 'early', delay: '−1' },
    { label: '13 min', state: 'cancelled', delay: null },
  ])('round-trips %o', (cell) => {
    const s = encodeCell(cell);
    expect(s).not.toMatch(/\|/);
    expect(decodeCell(s)).toEqual(cell);
  });

  it('reads unknown input as a timetable cell', () => {
    expect(decodeCell('3 min')).toEqual({ label: '3 min', state: 'scheduled', delay: null });
    expect(decodeCell('x\t3 min')).toEqual({ label: '3 min', state: 'scheduled', delay: null });
  });
});
