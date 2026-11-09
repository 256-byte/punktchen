// The global 1 s ticker (ARCHITECTURE §4.1, task 3.5). One interval for the
// whole app; components read "now" through useNow with a selector, so they
// re-render only when what they show changes (a countdown label: once a
// minute), not every second.
import { useEffect } from 'react';
import { AppState } from 'react-native';
import { useStore } from 'zustand';
import { useShallow } from 'zustand/react/shallow';
import { createStore } from 'zustand/vanilla';

export const TICK_MS = 1000;

export interface ClockState {
  /** Server-corrected time (epoch ms) as of the last tick. */
  now: number;
  /** Added to the device clock (ADR-0007, `Nearby.clockOffsetMs`). */
  offsetMs: number;
  tick(): void;
  setOffset(offsetMs: number): void;
}

export function createClockStore(deviceNow: () => number = Date.now) {
  return createStore<ClockState>()((set, get) => ({
    now: deviceNow(),
    offsetMs: 0,
    tick: () => set({ now: deviceNow() + get().offsetMs }),
    setOffset: (offsetMs) => {
      if (offsetMs !== get().offsetMs) {
        set({ offsetMs, now: deviceNow() + offsetMs });
      }
    },
  }));
}

export type ClockStore = ReturnType<typeof createClockStore>;

export const clockStore = createClockStore();

/**
 * A value derived from "now"; the component re-renders only when it changes
 * (shallow compare, so arrays of labels work).
 */
export function useNow<T>(select: (now: number) => T, store: ClockStore = clockStore): T {
  return useStore(
    store,
    useShallow((s) => select(s.now)),
  );
}

/**
 * Runs the ticker while mounted; ticks at once when the app returns to the
 * foreground (timers do not run in the background).
 */
export function useClockTicker(store: ClockStore = clockStore): void {
  useEffect(() => {
    const { tick } = store.getState();
    tick();
    const timer = setInterval(tick, TICK_MS);
    const sub = AppState.addEventListener('change', (state) => {
      if (state === 'active') tick();
    });
    return () => {
      clearInterval(timer);
      sub.remove();
    };
  }, [store]);
}
