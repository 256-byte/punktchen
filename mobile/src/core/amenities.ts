// Public toilets and drinking fountains (Phase 9, ADR-0010): what their
// markers and sheets say. Texts from the source are German and shown as
// they are; our own wording is English like the rest of the app.
import type { components } from '../api/schema.gen';

export type Toilet = components['schemas']['Toilet'];
export type Fountain = components['schemas']['Fountain'];
export type Season = components['schemas']['Season'];

/** Shown with toilet and fountain data (the licence does not require it). */
export const AMENITY_ATTRIBUTION = 'Data: Geoportal Berlin (dl-de/zero-2-0)';

const MONTHS = [
  'January',
  'February',
  'March',
  'April',
  'May',
  'June',
  'July',
  'August',
  'September',
  'October',
  'November',
  'December',
] as const;

/** Whether month (1–12) lies in the season; a season may wrap the year (Nov–Mar). */
export function inSeason(season: Season, month: number): boolean {
  const { fromMonth: from, toMonth: to } = season;
  return from <= to ? month >= from && month <= to : month >= from || month <= to;
}

/** "May–October". */
export function seasonText(season: Season): string {
  return `${MONTHS[season.fromMonth - 1]}–${MONTHS[season.toMonth - 1]}`;
}

export type FountainState = 'running' | 'offSeason' | 'outOfService';

export interface FountainStatus {
  state: FountainState;
  /** One line for the sheet, e.g. "Off season · runs May–October". */
  text: string;
}

/**
 * Whether a fountain runs now (user decision 2026-11-18: out of season or
 * out of service it is shown greyed with the reason). The month comes from
 * the device date: Berlin's month except in the hour around midnight of a
 * month's last day on a phone in another time zone.
 */
export function fountainStatus(
  f: Pick<Fountain, 'season' | 'outOfService'>,
  now: number,
): FountainStatus {
  if (f.outOfService) {
    return { state: 'outOfService', text: `Out of service: ${f.outOfService}` };
  }
  const season = seasonText(f.season);
  if (!inSeason(f.season, new Date(now).getMonth() + 1)) {
    return { state: 'offSeason', text: `Off season · runs ${season}` };
  }
  return { state: 'running', text: `Running · season ${season}` };
}

/**
 * The note of a toilet that is not open on a normal day (user decision
 * 2026-11-18: shown, with a note); null for all others.
 */
export function toiletNote(t: Pick<Toilet, 'eventOnly' | 'openingHours'>): string | null {
  if (!t.eventOnly) return null;
  return /trauerfeier/i.test(t.openingHours ?? '')
    ? 'Only open during funerals (with a key)'
    : 'Only open during events';
}

/** "Open 24 hours", the source's hours, or "Opening hours unknown". */
export function openingText(t: Pick<Toilet, 'open24h' | 'openingHours'>): string {
  if (t.open24h) return 'Open 24 hours';
  return t.openingHours ?? 'Opening hours unknown';
}

/** "Free", "€0.50" or "Fee unknown". */
export function feeText(fee: number | null): string {
  if (fee === null) return 'Fee unknown';
  if (fee === 0) return 'Free';
  return `€${fee.toFixed(2)}`;
}

const PAYMENTS: Record<string, string> = {
  'karte/app': 'Card or app only',
  bar: 'Cash',
  'bar/karte': 'Cash or card',
};

/** How to pay, in English where we know the source's wording; null when unknown. */
export function paymentText(payment: string | null): string | null {
  if (payment === null) return null;
  return PAYMENTS[payment.trim().toLowerCase()] ?? payment;
}

/** "Barrier-free", "Mostly barrier-free", "Not barrier-free" or unknown. */
export function accessibleText(t: Pick<Toilet, 'accessible' | 'mostlyAccessible'>): string {
  if (t.accessible) return 'Barrier-free';
  if (t.mostlyAccessible) return 'Mostly barrier-free';
  if (t.accessible === false && t.mostlyAccessible === false) return 'Not barrier-free';
  return 'Accessibility unknown';
}

/** "Changing table", "No changing table" or unknown. */
export function changingTableText(changingTable: boolean | null): string {
  if (changingTable === null) return 'Changing table unknown';
  return changingTable ? 'Changing table' : 'No changing table';
}

/**
 * About how far, from the cell centre (the API's distanceMeters, ADR-0006),
 * so rounded: "about 290 m", "about 1.2 km".
 */
export function distanceText(meters: number): string {
  if (meters < 1000) return `about ${Math.round(meters / 10) * 10} m`;
  return `about ${(meters / 1000).toFixed(1)} km`;
}

/** What a screen reader says for a toilet marker. */
export function toiletLabel(t: Toilet): string {
  const kind = t.kind === 'urinal' ? 'Public urinal' : 'Public toilet';
  return [
    `${kind}, ${t.address}`,
    feeText(t.feeEuro),
    accessibleText(t),
    toiletNote(t) ?? openingText(t),
  ].join('. ');
}

/** What a screen reader says for a fountain marker. */
export function fountainLabel(f: Fountain, now: number): string {
  return `Drinking fountain, ${f.place}. ${fountainStatus(f, now).text}`;
}
