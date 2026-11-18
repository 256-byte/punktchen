import { BottomSheetScrollView } from '@gorhom/bottom-sheet';
import { Text, View } from 'react-native';

import {
  accessibleText,
  AMENITY_ATTRIBUTION,
  changingTableText,
  distanceText,
  feeText,
  type Fountain,
  fountainStatus,
  openingText,
  paymentText,
  type Toilet,
  toiletNote,
} from '@/core/amenities';
import { type ClockStore, clockStore, useNow } from '@/state/clock';
import { Attribution } from '@/ui/Attribution';
import { themedStyles } from '@/ui/theme';

import { AmenityIcon, type AmenityKind } from './AmenityIcon';

interface Row {
  label: string;
  value: string;
}

/**
 * The details of one toilet or fountain (Phase 9). Everything comes with
 * the /v1/nearby answer: no request of its own. The note says why it
 * cannot be used now, if so.
 */
function AmenitySheet({
  kind,
  title,
  subtitle,
  note,
  rows,
}: {
  kind: AmenityKind;
  title: string;
  subtitle: string;
  note: string | null;
  rows: readonly Row[];
}) {
  const styles = useStyles();
  return (
    <BottomSheetScrollView contentContainerStyle={styles.content}>
      <View style={styles.titleRow} accessible accessibilityRole="header" testID="sheet-title">
        <AmenityIcon kind={kind} size={30} />
        <Text style={styles.title}>{title}</Text>
      </View>
      <Text style={styles.subtitle}>{subtitle}</Text>
      {note ? (
        <View style={styles.note} accessibilityRole="alert" testID="amenity-note">
          <Text style={styles.noteText}>{note}</Text>
        </View>
      ) : null}
      <View style={styles.rows}>
        {rows.map((r) => (
          <View
            key={r.label}
            style={styles.row}
            accessible
            accessibilityLabel={`${r.label}: ${r.value}`}
          >
            <Text style={styles.label}>{r.label}</Text>
            <Text style={styles.value}>{r.value}</Text>
          </View>
        ))}
      </View>
      <Attribution lines={[AMENITY_ATTRIBUTION]} style={styles.attribution} />
    </BottomSheetScrollView>
  );
}

function subtitle(district: string | null, meters: number): string {
  return [district, distanceText(meters)].filter(Boolean).join(' · ');
}

export function ToiletSheet({ feature: t }: { feature: Toilet }) {
  const rows: Row[] = [
    { label: 'Opening hours', value: openingText(t) },
    { label: 'Fee', value: feeText(t.feeEuro) },
  ];
  const payment = paymentText(t.payment);
  if (payment !== null) rows.push({ label: 'Payment', value: payment });
  rows.push(
    { label: 'Access', value: accessibleText(t) },
    { label: 'Baby care', value: changingTableText(t.changingTable) },
  );
  if (t.operator !== null) rows.push({ label: 'Operator', value: t.operator });
  return (
    <AmenitySheet
      kind="toilet"
      title={t.kind === 'urinal' ? `Urinal · ${t.address}` : t.address}
      subtitle={subtitle(t.district, t.distanceMeters)}
      note={toiletNote(t)}
      rows={rows}
    />
  );
}

export function FountainSheet({
  feature: f,
  clock = clockStore,
}: {
  feature: Fountain;
  clock?: ClockStore;
}) {
  // A plain string: the sheet re-renders when the month's verdict changes.
  const status = useNow((now) => fountainStatus(f, now).text, clock);
  const running = status.startsWith('Running');
  const rows: Row[] = [{ label: 'Status', value: status }];
  if (f.model !== null) rows.push({ label: 'Model', value: f.model });
  if (f.builtYear !== null) rows.push({ label: 'Built', value: String(f.builtYear) });
  return (
    <AmenitySheet
      kind="fountain"
      title={`Drinking fountain · ${f.place}`}
      subtitle={subtitle(f.district, f.distanceMeters)}
      note={running ? null : status}
      rows={rows}
    />
  );
}

const useStyles = themedStyles(({ colors, fontSize, fontWeight, radius, space }) => ({
  content: {
    padding: space.xl,
    gap: space.lg,
  },
  titleRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.ml,
  },
  title: {
    flex: 1,
    fontSize: fontSize.title,
    fontWeight: fontWeight.semibold,
    color: colors.text,
  },
  subtitle: {
    fontSize: fontSize.callout,
    color: colors.textSecondary,
  },
  note: {
    padding: space.lg,
    borderRadius: radius.lg,
    borderWidth: 1,
    backgroundColor: colors.warningSurface,
    borderColor: colors.warningBorder,
  },
  noteText: {
    fontSize: fontSize.body,
    color: colors.warningText,
  },
  rows: {
    gap: space.md,
  },
  row: {
    flexDirection: 'row',
    gap: space.lg,
    paddingVertical: space.xs,
    borderBottomWidth: 1,
    borderBottomColor: colors.border,
  },
  label: {
    width: 110,
    fontSize: fontSize.body,
    color: colors.textSecondary,
  },
  value: {
    flex: 1,
    fontSize: fontSize.body,
    color: colors.text,
  },
  attribution: {
    marginTop: space.md,
  },
}));
