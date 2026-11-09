// Stand-in for @gorhom/bottom-sheet (it needs Reanimated's native side):
// BottomSheet renders its children and records its props, so tests can
// "swipe it down" by calling onClose.
import type { ReactNode } from 'react';
import { ScrollView, View } from 'react-native';

type SheetProps = {
  children?: ReactNode;
  onClose?: () => void;
  index?: number;
  snapPoints?: (string | number)[];
  enablePanDownToClose?: boolean;
};

export const sheet = { props: null as SheetProps | null };

function record(props: SheetProps) {
  sheet.props = props;
}

function BottomSheet(props: SheetProps) {
  record(props);
  return <View testID="bottom-sheet">{props.children}</View>;
}

function BottomSheetScrollView({ children }: { children?: ReactNode }) {
  return <ScrollView>{children}</ScrollView>;
}

export const bottomSheetMock = { __esModule: true, default: BottomSheet, BottomSheetScrollView };
