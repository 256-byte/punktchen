// Stand-in for react-native-maps (jest.mock factory): MapView records its
// props and camera calls; Marker renders its title and children; the
// projection is a flat grid of 1 point per 0.00001° (≈ 1.1 m north–south),
// so stops 20 m apart are about 18 points apart on the "screen".
import { jest } from '@jest/globals';
import { forwardRef, type ReactNode, useImperativeHandle } from 'react';
import { Pressable, Text, View } from 'react-native';
import type { Camera, LatLng, MapViewProps, Point, Region } from 'react-native-maps';

type Props = MapViewProps & { children?: ReactNode };

export const map = {
  props: null as Props | null,
  /** Points per degree: set it to stand for a zoom level (default ≈ street zoom). */
  scale: 100_000,
  animateToRegion: jest.fn<(region: Region, duration?: number) => void>(),
  animateCamera: jest.fn<(camera: Partial<Camera>, opts?: { duration?: number }) => void>(),
  pointForCoordinate: async (c: LatLng): Promise<Point> => ({
    x: (c.longitude - 13) * map.scale,
    y: (53 - c.latitude) * map.scale,
  }),
  coordinateForPoint: async (p: Point): Promise<LatLng> => ({
    latitude: 53 - p.y / map.scale,
    longitude: 13 + p.x / map.scale,
  }),
};

// Outside the component: the React Compiler lint forbids writing module
// state during render, and a test spy has to.
function record(props: Props) {
  map.props = props;
}

const MapView = forwardRef<unknown, Props>(function MapView(props, ref) {
  record(props);
  useImperativeHandle(ref, () => ({
    animateToRegion: map.animateToRegion,
    animateCamera: map.animateCamera,
    pointForCoordinate: map.pointForCoordinate,
    coordinateForPoint: map.coordinateForPoint,
  }));
  return <View testID="map">{props.children}</View>;
});

/**
 * Renders its children; tracksViewChanges and coordinate are readable from
 * the host view's props; a press on it calls onPress (as tapping a map
 * marker does).
 */
function Marker({
  testID = 'marker',
  title,
  coordinate,
  tracksViewChanges,
  onPress,
  children,
}: {
  testID?: string;
  title?: string;
  coordinate?: LatLng;
  tracksViewChanges?: boolean;
  onPress?: () => void;
  children?: ReactNode;
}) {
  return (
    <Pressable testID={testID} onPress={onPress} {...{ tracksViewChanges, coordinate }}>
      {title ? <Text>pin {title}</Text> : null}
      {children}
    </Pressable>
  );
}

export const mapsMock = { __esModule: true, default: MapView, Marker };
