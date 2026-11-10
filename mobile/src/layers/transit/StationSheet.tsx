import { BottomSheetScrollView } from '@gorhom/bottom-sheet';
import { useMemo } from 'react';
import { Text, View } from 'react-native';

import type { ApiClient } from '@/api/client';
import { useStation } from '@/api/station';
import { type Direction, directionHeadsign, MODE_NAMES, ringSymbol } from '@/core/directions';
import { cellDescription } from '@/core/realtime';
import {
  poleTitle,
  type SheetPole,
  type SheetRow,
  sheetModel,
  sheetSignature,
  stationModes,
} from '@/core/sheet';
import { freshness } from '@/core/staleness';
import { type ClockStore, clockStore, useDeviceNow, useNow } from '@/state/clock';
import { Attribution } from '@/ui/Attribution';
import { themedStyles, useTheme } from '@/ui/theme';
import type { ColorScheme } from '@/ui/tokens';

import { DepartureTime } from './DepartureTime';
import type { PoleFeature } from './index';
import { LineBadge } from './LineBadge';
import { ModeIcon } from './ModeIcon';

/**
 * The station sheet (task 3.6): all poles of the tapped pole's station
 * (/v1/stations/{id}), each with its running directions and their next
 * departures; directions with nothing coming at the bottom (user decision
 * 2026-11-09). Until the station loads it shows what /v1/nearby had. Live
 * times of data older than 5 min are greyed out (task 6.2): by the age of
 * the station answer, or of the /v1/nearby one (`stale`) while that is shown.
 */
export function StationSheet({
  feature,
  client,
  stale: nearbyStale = false,
  clock = clockStore,
}: {
  feature: PoleFeature;
  client: ApiClient | null;
  stale?: boolean;
  clock?: ClockStore;
}) {
  const query = useStation(client, feature.station.id);
  const station = query.data?.response.station ?? feature.station;
  const signature = useNow((now) => sheetSignature(station, now), clock);
  const model = useMemo(() => sheetModel(station, signature), [station, signature]);
  const modes = useMemo(() => stationModes(station), [station]);
  const loading = query.isPending && client !== null;
  const checkedAt = query.data?.checkedAt;
  const ownStale = useDeviceNow(
    (now) => (checkedAt === undefined ? null : freshness(checkedAt, now) === 'stale'),
    clock,
  );
  const stale = ownStale ?? nearbyStale;
  const styles = useStyles();
  const { scheme } = useTheme();

  return (
    <BottomSheetScrollView contentContainerStyle={styles.content}>
      {/* Every mode that stops here, then the name (task 7.10). */}
      <View
        style={styles.titleRow}
        accessible
        accessibilityRole="header"
        accessibilityLabel={[station.name, ...modes.map((m) => MODE_NAMES[m])].join(', ')}
        testID="sheet-title"
      >
        {modes.length > 0 ? (
          <View style={styles.titleIcons}>
            {modes.map((m) => (
              <ModeIcon key={m} mode={m} size={SHEET_ICON_SIZE} />
            ))}
          </View>
        ) : null}
        <Text style={styles.title}>{station.name}</Text>
      </View>
      {loading ? <Text style={styles.note}>Loading all platforms…</Text> : null}
      {query.isError ? (
        <Text style={[styles.note, styles.error]} accessibilityRole="alert">
          Could not load the whole station: {query.error.message}
        </Text>
      ) : null}
      {model.poles.map((p, i) => (
        <PoleSection key={p.pole.id} pole={p} index={i} stale={stale} />
      ))}
      {model.poles.length === 0 && !loading ? (
        <Text style={styles.note}>
          {stale
            ? 'The saved departures have left. Newer ones load when the connection is back.'
            : 'No departures in the next 90 minutes.'}
        </Text>
      ) : null}
      {model.idle.length > 0 ? (
        <View style={styles.section}>
          {/* Stale data ran out: nothing is known about later departures (task 6.3). */}
          <Text style={styles.sectionTitle}>{stale ? 'No newer data' : 'No departures soon'}</Text>
          {model.idle.map((r) => (
            <View
              key={r.key}
              style={[styles.row, styles.idle]}
              accessibilityLabel={`${r.direction.line} to ${directionHeadsign(r.direction)}${r.platform ? `, platform ${r.platform}` : ''}: ${stale ? 'no newer data' : 'no departures soon'}`}
            >
              <LineBadge direction={r.direction} size="large" />
              <HeadsignText direction={r.direction} />
              {r.platform ? <Text style={styles.platform}>Pl. {r.platform}</Text> : null}
            </View>
          ))}
        </View>
      ) : null}
      {model.poles.length > 0 ? <Text style={styles.legend}>{legend(stale, scheme)}</Text> : null}
      <Attribution lines={query.data?.response.meta.attribution ?? []} />
    </BottomSheetScrollView>
  );
}

/** Mode icons in the sheet's title: the height of its capital letters and a bit more. */
const SHEET_ICON_SIZE = 22;

/** The colour legend under the departures; timetable times are the text colour. */
function legend(stale: boolean, scheme: ColorScheme): string {
  const plain = scheme === 'dark' ? 'White' : 'Black';
  return stale
    ? `Grey: live times from more than 5 minutes ago, may be out of date. ${plain}: timetable only.`
    : `Green: live, on time. Red: live and late (+2 = 2 min). ${plain}: timetable only.`;
}

function PoleSection({ pole, index, stale }: { pole: SheetPole; index: number; stale: boolean }) {
  const styles = useStyles();
  const title = poleTitle(pole.pole, index);
  return (
    <View style={styles.section}>
      <Text style={styles.sectionTitle}>{title}</Text>
      {pole.rows.map((r) => (
        <DirectionRow key={r.key} row={r} stale={stale} />
      ))}
    </View>
  );
}

function DirectionRow({ row, stale }: { row: SheetRow; stale: boolean }) {
  const styles = useStyles();
  const label = `${row.direction.line} to ${directionHeadsign(row.direction)}: ${row.departures.map((d) => cellDescription(d, stale)).join('; ')}`;
  return (
    <View style={styles.row} accessibilityLabel={label}>
      <LineBadge direction={row.direction} size="large" />
      <HeadsignText direction={row.direction} />
      <View style={styles.times}>
        {row.departures.map((d, i) => (
          <DepartureTime
            key={i}
            cell={d}
            style={[styles.time, i === 0 && styles.next]}
            stale={stale}
          />
        ))}
      </View>
    </View>
  );
}

function HeadsignText({ direction }: { direction: Direction }) {
  const styles = useStyles();
  const ring = ringSymbol(direction);
  return (
    <Text style={styles.headsign} numberOfLines={1}>
      {ring ? (
        <>
          <Text style={styles.ringSymbol}>{`${ring} `}</Text>
          {direction.headsign}
        </>
      ) : (
        direction.headsign
      )}
    </Text>
  );
}

const useStyles = themedStyles(({ colors, fontSize, fontWeight, opacity, space }) => ({
  content: {
    paddingHorizontal: space.xl,
    paddingBottom: space.xxxl,
    gap: space.lg,
  },
  titleRow: {
    flexDirection: 'row',
    alignItems: 'center',
    flexWrap: 'wrap',
    gap: space.sm,
  },
  titleIcons: {
    flexDirection: 'row',
    gap: space.xs,
  },
  title: {
    flexShrink: 1,
    fontSize: fontSize.title,
    fontWeight: fontWeight.bold,
    color: colors.text,
  },
  note: {
    fontSize: fontSize.callout,
    color: colors.textSecondary,
  },
  error: {
    color: colors.danger,
  },
  section: {
    gap: space.md,
  },
  sectionTitle: {
    fontSize: fontSize.footnote,
    fontWeight: fontWeight.semibold,
    textTransform: 'uppercase',
    color: colors.textTertiary,
  },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.ml,
  },
  idle: {
    opacity: opacity.dimmed,
  },
  headsign: {
    flex: 1,
    fontSize: fontSize.body,
    lineHeight: fontSize.title,
    color: colors.text,
  },
  ringSymbol: {
    // U+27F2/U+27F3 glyphs sit at ~65 % of the em box in the system symbol font;
    // scaling up brings ⟳/⟲ to the cap height of the body text.
    fontSize: fontSize.titleLarge,
    lineHeight: fontSize.title,
    fontWeight: fontWeight.bold,
  },
  platform: {
    fontSize: fontSize.footnote,
    color: colors.textSecondary,
  },
  times: {
    flexDirection: 'row',
    gap: space.ml,
  },
  time: {
    fontSize: fontSize.callout,
    color: colors.textSecondary,
    fontVariant: ['tabular-nums'],
  },
  next: {
    fontWeight: fontWeight.bold,
    color: colors.text,
  },
  legend: {
    fontSize: fontSize.caption,
    color: colors.textTertiary,
  },
}));
