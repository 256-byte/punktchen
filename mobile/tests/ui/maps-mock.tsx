// Stand-in for react-native-maps (jest.mock factory): MapView records its
// props and camera calls; Marker renders its title and children.
import { jest } from '@jest/globals';
import { forwardRef, type ReactNode, useImperativeHandle } from 'react';
import { Pressable, Text, View } from 'react-native';
import type { Camera, MapViewProps, Region } from 'react-native-maps';

type Props = MapViewProps & { children?: ReactNode };

export const map = {
  props: null as Props | null,
  animateToRegion: jest.fn<(region: Region, duration?: number) => void>(),
  animateCamera: jest.fn<(camera: Partial<Camera>, opts?: { duration?: number }) => void>(),
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
  }));
  return <View testID="map">{props.children}</View>;
});

/**
 * Renders its children; tracksViewChanges is readable from the host view's
 * props; a press on it calls onPress (as tapping a map marker does).
 */
function Marker({
  title,
  tracksViewChanges,
  onPress,
  children,
}: {
  title?: string;
  tracksViewChanges?: boolean;
  onPress?: () => void;
  children?: ReactNode;
}) {
  return (
    <Pressable testID="marker" onPress={onPress} {...{ tracksViewChanges }}>
      {title ? <Text>pin {title}</Text> : null}
      {children}
    </Pressable>
  );
}

export const mapsMock = { __esModule: true, default: MapView, Marker };
