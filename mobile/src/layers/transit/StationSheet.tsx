import { BottomSheetScrollView } from '@gorhom/bottom-sheet';
import { useMemo } from 'react';
import { StyleSheet, Text, View } from 'react-native';

import type { ApiClient } from '@/api/client';
import { useStation } from '@/api/station';
import { poleTitle, type SheetPole, type SheetRow, sheetModel, sheetSignature } from '@/core/sheet';
import { type ClockStore, clockStore, useNow } from '@/state/clock';
import { Attribution } from '@/ui/Attribution';

import type { PoleFeature } from './index';
import { LineBadge } from './LineBadge';

/**
 * The station sheet (task 3.6): all poles of the tapped pole's station
 * (/v1/stations/{id}), each with its running directions and their next
 * departures; directions with nothing coming at the bottom (user decision
 * 2026-11-09). Until the station loads it shows what /v1/nearby had.
 */
export function StationSheet({
  feature,
  client,
  clock = clockStore,
}: {
  feature: PoleFeature;
  client: ApiClient | null;
  clock?: ClockStore;
}) {
  const query = useStation(client, feature.station.id);
  const station = query.data?.response.station ?? feature.station;
  const signature = useNow((now) => sheetSignature(station, now), clock);
  const model = useMemo(() => sheetModel(station, signature), [station, signature]);
  const loading = query.isPending && client !== null;

  return (
    <BottomSheetScrollView contentContainerStyle={styles.content}>
      <Text style={styles.title} accessibilityRole="header">
        {station.name}
      </Text>
      {loading ? <Text style={styles.note}>Loading all platforms…</Text> : null}
      {query.isError ? (
        <Text style={[styles.note, styles.error]}>
          Could not load the whole station: {query.error.message}
        </Text>
      ) : null}
      {model.poles.map((p, i) => (
        <PoleSection key={p.pole.id} pole={p} index={i} />
      ))}
      {model.poles.length === 0 && !loading ? (
        <Text style={styles.note}>No departures in the next 90 minutes.</Text>
      ) : null}
      {model.idle.length > 0 ? (
        <View style={styles.section}>
          <Text style={styles.sectionTitle}>No departures soon</Text>
          {model.idle.map((r) => (
            <View key={r.key} style={[styles.row, styles.idle]}>
              <LineBadge direction={r.direction} size="large" />
              <Text style={styles.headsign} numberOfLines={1}>
                {r.direction.headsign}
              </Text>
              {r.platform ? <Text style={styles.platform}>Pl. {r.platform}</Text> : null}
            </View>
          ))}
        </View>
      ) : null}
      <Attribution lines={query.data?.response.meta.attribution ?? []} />
    </BottomSheetScrollView>
  );
}

function PoleSection({ pole, index }: { pole: SheetPole; index: number }) {
  const title = poleTitle(pole.pole, index);
  return (
    <View style={styles.section}>
      <Text style={styles.sectionTitle}>{title}</Text>
      {pole.rows.map((r) => (
        <DirectionRow key={r.key} row={r} />
      ))}
    </View>
  );
}

function DirectionRow({ row }: { row: SheetRow }) {
  return (
    <View style={styles.row}>
      <LineBadge direction={row.direction} size="large" />
      <Text style={styles.headsign} numberOfLines={1}>
        {row.direction.headsign}
      </Text>
      <View style={styles.times}>
        {row.departures.map((d, i) => (
          <Text
            key={i}
            style={[styles.time, i === 0 && styles.next, d.cancelled && styles.cancelled]}
            accessibilityLabel={d.cancelled ? `${d.label}, cancelled` : d.label}
          >
            {d.label}
          </Text>
        ))}
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  content: {
    paddingHorizontal: 16,
    paddingBottom: 32,
    gap: 12,
  },
  title: {
    fontSize: 20,
    fontWeight: '700',
    color: '#111111',
  },
  note: {
    fontSize: 14,
    color: '#555555',
  },
  error: {
    color: '#b42318',
  },
  section: {
    gap: 8,
  },
  sectionTitle: {
    fontSize: 13,
    fontWeight: '600',
    textTransform: 'uppercase',
    color: '#666666',
  },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 10,
  },
  idle: {
    opacity: 0.5,
  },
  headsign: {
    flex: 1,
    fontSize: 15,
    color: '#111111',
  },
  platform: {
    fontSize: 13,
    color: '#555555',
  },
  times: {
    flexDirection: 'row',
    gap: 10,
  },
  time: {
    fontSize: 14,
    color: '#444444',
    fontVariant: ['tabular-nums'],
  },
  next: {
    fontWeight: '700',
    color: '#111111',
  },
  cancelled: {
    textDecorationLine: 'line-through',
    color: '#b42318',
  },
});
