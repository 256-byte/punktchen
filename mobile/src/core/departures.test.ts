import { describe, expect, it } from 'vitest';

import {
  type Departure,
  earliestDepartureAt,
  effectiveDeparture,
  hasDeparted,
  nextDeparture,
  upcomingDepartures,
} from './departures';
import { MINUTE, SECOND } from './time';

const now = Date.parse('2026-11-05T14:30:00+01:00');

function scheduled(time: string, extra: Partial<Departure> = {}): Departure {
  return {
    tripId: `t-${time}`,
    serviceDate: '2026-11-05',
    scheduled: `2026-11-05T${time}:00+01:00`,
    predicted: null,
    delaySeconds: null,
    source: 'scheduled',
    cancelled: false,
    ...extra,
  };
}

function delayed(time: string, predicted: string): Departure {
  const delaySeconds =
    (Date.parse(`2026-11-05T${predicted}:00+01:00`) - Date.parse(`2026-11-05T${time}:00+01:00`)) /
    1000;
  return scheduled(time, {
    predicted: `2026-11-05T${predicted}:00+01:00`,
    delaySeconds,
    source: 'realtime',
  });
}

const ids = (ds: { tripId: string }[]) => ds.map((d) => d.tripId);

describe('hasDeparted', () => {
  const dep = Date.parse('2026-11-05T14:32:00+01:00');

  it('keeps a departure through its minute', () => {
    expect(hasDeparted(dep, dep - MINUTE)).toBe(false);
    expect(hasDeparted(dep, dep)).toBe(false);
    expect(hasDeparted(dep, dep + 59 * SECOND)).toBe(false);
  });

  it('drops it after that', () => {
    expect(hasDeparted(dep, dep + MINUTE)).toBe(true);
    expect(hasDeparted(dep, dep + 10 * MINUTE)).toBe(true);
  });
});

describe('effectiveDeparture', () => {
  it('uses the schedule without a prediction', () => {
    const d = scheduled('14:33');
    expect(effectiveDeparture(d, now)).toEqual({
      ...d,
      effectiveAt: Date.parse('2026-11-05T14:33:00+01:00'),
      minutesLeft: 3,
      label: '3 min',
    });
  });

  it('uses the prediction when there is one', () => {
    const e = effectiveDeparture(delayed('14:33', '14:36'), now);
    expect(e).toMatchObject({
      effectiveAt: Date.parse('2026-11-05T14:36:00+01:00'),
      minutesLeft: 6,
      label: '6 min',
    });
  });

  it('shows departures from 60 min on as clock time of the effective time', () => {
    expect(effectiveDeparture(delayed('15:28', '15:31'), now)?.label).toBe('15:31');
    expect(effectiveDeparture(scheduled('15:29'), now)?.label).toBe('59 min');
  });

  it('returns null for an invalid timestamp', () => {
    expect(effectiveDeparture(scheduled('14:33', { scheduled: 'later' }), now)).toBeNull();
    expect(
      effectiveDeparture(scheduled('14:33', { predicted: '2026-11-05T14:34:00' }), now),
    ).toBeNull();
  });
});

describe('upcomingDepartures', () => {
  it('drops departed and invalid ones and orders by effective time', () => {
    const list = [
      scheduled('14:28'), // left
      scheduled('14:29'), // leaves during 14:29, gone at 14:30
      delayed('14:25', '14:30'), // late, leaving now
      scheduled('14:35'),
      delayed('14:31', '14:38'), // overtaken by 14:35
      scheduled('14:40', { scheduled: 'x' }),
    ];
    const up = upcomingDepartures(list, now);
    expect(ids(up)).toEqual(['t-14:25', 't-14:35', 't-14:31']);
    expect(up.map((d) => d.label)).toEqual(['now', '5 min', '8 min']);
  });

  it('keeps cancelled ones and the API order for equal times', () => {
    const list = [
      scheduled('14:34', { tripId: 'b' }),
      scheduled('14:34', { tripId: 'a', cancelled: true }),
    ];
    expect(ids(upcomingDepartures(list, now))).toEqual(['b', 'a']);
  });

  it('returns an empty list for no departures', () => {
    expect(upcomingDepartures([], now)).toEqual([]);
  });
});

describe('nextDeparture', () => {
  it('skips cancelled departures', () => {
    const list = [scheduled('14:32', { cancelled: true }), scheduled('14:41')];
    expect(nextDeparture(list, now)?.tripId).toBe('t-14:41');
  });

  it('is undefined when nothing runs any more', () => {
    expect(
      nextDeparture([scheduled('14:10'), scheduled('14:45', { cancelled: true })], now),
    ).toBeUndefined();
  });
});

describe('earliestDepartureAt', () => {
  it('takes the earliest next departure over all lists', () => {
    const lists = [
      [scheduled('14:39')],
      [scheduled('14:20'), delayed('14:31', '14:34')],
      [scheduled('14:33', { cancelled: true })],
      [],
    ];
    expect(earliestDepartureAt(lists, now)).toBe(Date.parse('2026-11-05T14:34:00+01:00'));
  });

  it('is null without any departure', () => {
    expect(earliestDepartureAt([], now)).toBeNull();
    expect(earliestDepartureAt([[], [scheduled('14:00')]], now)).toBeNull();
  });
});
