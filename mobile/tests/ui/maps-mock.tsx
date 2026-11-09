// Stand-in for react-native-maps (jest.mock factory): MapView records its
// props and camera calls; Marker renders its title and children.
import { jest } from '@jest/globals';
import { forwardRef, type ReactNode, useImperativeHandle } from 'react';
import { Text, View } from 'react-native';
import type { MapViewProps, Region } from 'react-native-maps';

type Props = MapViewProps & { children?: ReactNode };

export const map = {
  props: null as Props | null,
  animateToRegion: jest.fn<(region: Region, duration?: number) => void>(),
};

// Outside the component: the React Compiler lint forbids writing module
// state during render, and a test spy has to.
function record(props: Props) {
  map.props = props;
}

const MapView = forwardRef<unknown, Props>(function MapView(props, ref) {
  record(props);
  useImperativeHandle(ref, () => ({ animateToRegion: map.animateToRegion }));
  return <View testID="map">{props.children}</View>;
});

/** Renders its children; tracksViewChanges is readable from the host view's props. */
function Marker({
  title,
  tracksViewChanges,
  children,
}: {
  title?: string;
  tracksViewChanges?: boolean;
  children?: ReactNode;
}) {
  return (
    <View testID="marker" {...{ tracksViewChanges }}>
      {title ? <Text>pin {title}</Text> : null}
      {children}
    </View>
  );
}

export const mapsMock = { __esModule: true, default: MapView, Marker };
