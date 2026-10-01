import { useCallback, useState } from "react";

export type TimeWindow = {
  t0: number;
  t1: number;
  span: number;
  center: (t: number) => void;
  zoom: (factor: number, anchor?: number) => void;
  setSpan: (span: number) => void;
  pan: (dt: number) => void;
  show: (t0: number, t1: number) => void;
};

const MIN_SPAN = 200; // 0.2 ms

/** A [t0, t1] view over [0, duration], in virtual microseconds. */
export function useTimeWindow(duration: number, focus: number, initialSpan: number): TimeWindow {
  const clamp = useCallback(
    (t0: number, span: number): [number, number] => {
      const s = Math.min(Math.max(span, MIN_SPAN), duration);
      const a = Math.min(Math.max(t0, 0), duration - s);
      return [a, a + s];
    },
    [duration],
  );
  const [w, setW] = useState<[number, number]>(() => clamp(focus - initialSpan / 2, initialSpan));
  const [t0, t1] = w;
  const span = t1 - t0;

  return {
    t0,
    t1,
    span,
    center: (t) => setW(([a, b]) => clamp(t - (b - a) / 2, b - a)),
    zoom: (factor, anchor) =>
      setW(([a, b]) => {
        const at = anchor ?? (a + b) / 2;
        const s = (b - a) * factor;
        const ratio = (at - a) / (b - a);
        return clamp(at - ratio * s, s);
      }),
    setSpan: (s) => setW(([a, b]) => clamp((a + b) / 2 - s / 2, s)),
    pan: (dt) => setW(([a, b]) => clamp(a + dt, b - a)),
    show: (a, b) => setW(clamp(a, b - a)),
  };
}
