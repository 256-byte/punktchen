import { describe, expect, it } from 'vitest';

import {
  accessibleText,
  changingTableText,
  distanceText,
  type Fountain,
  feeText,
  fountainLabel,
  fountainStatus,
  inSeason,
  openingText,
  paymentText,
  seasonText,
  type Toilet,
  toiletLabel,
  toiletNote,
} from './amenities';

const MAY_OCT = { fromMonth: 5, toMonth: 10 };
const at = (iso: string) => new Date(iso).getTime();

const toilet = (over: Partial<Toilet> = {}): Toilet => ({
  id: 'toiletten.2',
  kind: 'toilet',
  location: { lat: 52.52146, lng: 13.41295 },
  distanceMeters: 158,
  address: 'WC-Center Alexanderplatz 1',
  district: 'Mitte',
  operator: 'Wall GmbH',
  eventOnly: false,
  openingHours: 'Mo-Sa 07:00-22:00\nSo 10:00-22:00',
  open24h: false,
  feeEuro: 0.5,
  payment: 'Bar',
  accessible: false,
  mostlyAccessible: true,
  changingTable: true,
  ...over,
});

const fountain = (over: Partial<Fountain> = {}): Fountain => ({
  id: 'trinkwasserbrunnen.1',
  location: { lat: 52.43, lng: 13.41 },
  distanceMeters: 555,
  place: 'Poststr. 11 (Nikolaiviertel)',
  district: 'Mitte',
  model: 'Kaiser',
  builtYear: 2019,
  season: MAY_OCT,
  outOfService: null,
  ...over,
});

describe('season', () => {
  it('knows the months of a season, also across the new year', () => {
    expect([4, 5, 10, 11].map((m) => inSeason(MAY_OCT, m))).toEqual([false, true, true, false]);
    const winter = { fromMonth: 11, toMonth: 3 };
    expect([10, 11, 1, 3, 4].map((m) => inSeason(winter, m))).toEqual([
      false,
      true,
      true,
      true,
      false,
    ]);
    expect(seasonText(MAY_OCT)).toBe('May–October');
  });
});

describe('fountainStatus', () => {
  it('runs in season', () => {
    expect(fountainStatus(fountain(), at('2026-07-01T12:00:00+02:00'))).toEqual({
      state: 'running',
      text: 'Running · season May–October',
    });
  });

  it('is off season in November (user decision: greyed with the reason)', () => {
    expect(fountainStatus(fountain(), at('2026-11-18T12:00:00+01:00'))).toEqual({
      state: 'offSeason',
      text: 'Off season · runs May–October',
    });
  });

  it('out of service wins over the season', () => {
    const f = fountain({ outOfService: 'zur Zeit wegen Reparatur außer Betrieb' });
    expect(fountainStatus(f, at('2026-07-01T12:00:00+02:00'))).toEqual({
      state: 'outOfService',
      text: 'Out of service: zur Zeit wegen Reparatur außer Betrieb',
    });
  });

  it('says it for screen readers', () => {
    expect(fountainLabel(fountain(), at('2026-11-18T12:00:00+01:00'))).toBe(
      'Drinking fountain, Poststr. 11 (Nikolaiviertel). Off season · runs May–October',
    );
  });
});

describe('toilet texts', () => {
  it('notes toilets that are only open at events or funerals', () => {
    expect(toiletNote(toilet())).toBeNull();
    expect(toiletNote(toilet({ eventOnly: true, openingHours: 'bei Veranstaltungen' }))).toBe(
      'Only open during events',
    );
    expect(
      toiletNote(toilet({ eventOnly: true, openingHours: 'Mit Schlüssel während Trauerfeiern' })),
    ).toBe('Only open during funerals (with a key)');
    expect(toiletNote(toilet({ eventOnly: true, openingHours: null }))).toBe(
      'Only open during events',
    );
  });

  it('describes opening hours, fee and payment', () => {
    expect(openingText(toilet({ open24h: true, openingHours: '24h' }))).toBe('Open 24 hours');
    expect(openingText(toilet())).toBe('Mo-Sa 07:00-22:00\nSo 10:00-22:00');
    expect(openingText(toilet({ openingHours: null }))).toBe('Opening hours unknown');
    expect([0, 0.5, 1, null].map(feeText)).toEqual(['Free', '€0.50', '€1.00', 'Fee unknown']);
    expect(['Karte/App', 'Bar', 'Bar/Karte', 'Bar/Karte/Paypal', null].map(paymentText)).toEqual([
      'Card or app only',
      'Cash',
      'Cash or card',
      'Bar/Karte/Paypal',
      null,
    ]);
  });

  it('describes accessibility and the changing table', () => {
    expect(accessibleText({ accessible: true, mostlyAccessible: null })).toBe('Barrier-free');
    expect(accessibleText({ accessible: false, mostlyAccessible: true })).toBe(
      'Mostly barrier-free',
    );
    expect(accessibleText({ accessible: false, mostlyAccessible: false })).toBe('Not barrier-free');
    expect(accessibleText({ accessible: null, mostlyAccessible: null })).toBe(
      'Accessibility unknown',
    );
    expect([true, false, null].map(changingTableText)).toEqual([
      'Changing table',
      'No changing table',
      'Changing table unknown',
    ]);
  });

  it('rounds distances, they are from the cell centre', () => {
    expect([16, 158, 994, 1234].map(distanceText)).toEqual([
      'about 20 m',
      'about 160 m',
      'about 990 m',
      'about 1.2 km',
    ]);
  });

  it('says a marker for screen readers', () => {
    expect(toiletLabel(toilet())).toBe(
      'Public toilet, WC-Center Alexanderplatz 1. €0.50. Mostly barrier-free. Mo-Sa 07:00-22:00\nSo 10:00-22:00',
    );
    expect(
      toiletLabel(
        toilet({
          kind: 'urinal',
          address: 'Olympiastadion',
          eventOnly: true,
          openingHours: 'bei Veranstaltungen',
          feeEuro: 0,
          accessible: null,
          mostlyAccessible: null,
        }),
      ),
    ).toBe('Public urinal, Olympiastadion. Free. Accessibility unknown. Only open during events');
  });
});
