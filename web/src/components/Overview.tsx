import { useRef } from "react";
import type { Fault } from "../lib/api";
import type { Model } from "../lib/trace";
import type { TimeWindow } from "../lib/useTimeWindow";

/** Whole-run strip: message density, faults, the failure, and the window. */
export function Overview({ model, faults, win }: { model: Model; faults: Fault[]; win: TimeWindow }) {
  const ref = useRef<SVGSVGElement>(null);
  const dragging = useRef(false);
  const W = 1000;
  const H = 44;
  const max = Math.max(1, ...model.density);
  const bw = W / model.density.length;
  const x = (t: number) => (t / model.duration) * W;

  const moveTo = (clientX: number) => {
    const r = ref.current!.getBoundingClientRect();
    win.center(((clientX - r.left) / r.width) * model.duration);
  };

  return (
    <svg
      ref={ref}
      viewBox={`0 0 ${W} ${H}`}
      preserveAspectRatio="none"
      className="h-11 w-full cursor-pointer touch-none rounded-lg border border-ink-700 bg-ink-900"
      onPointerDown={(e) => {
        dragging.current = true;
        e.currentTarget.setPointerCapture(e.pointerId);
        moveTo(e.clientX);
      }}
      onPointerMove={(e) => dragging.current && moveTo(e.clientX)}
      onPointerUp={() => (dragging.current = false)}
      role="slider"
      aria-label="Janela de tempo"
      aria-valuemin={0}
      aria-valuemax={model.duration}
      aria-valuenow={win.t0}
    >
      {model.density.map((d, i) => (
        <rect key={i} x={i * bw} y={H - 6 - (d / max) * (H - 14)} width={bw - 0.5} height={(d / max) * (H - 14)} fill="#2b3542" />
      ))}
      {faults
        .filter((f) => !f.disabled)
        .map((f) => (
          <rect
            key={f.id}
            x={x(f.at)}
            y={f.kind === "crash" ? H - 4 : 0}
            width={Math.max(1, x(f.until) - x(f.at))}
            height={4}
            fill={f.kind === "crash" ? "#ff6b6b" : "#ffb547"}
            opacity={0.8}
          />
        ))}
      {model.focus > 0 && <line x1={x(model.focus)} x2={x(model.focus)} y1={0} y2={H} stroke="#ff6b6b" strokeWidth={2} />}
      <rect
        x={x(win.t0)}
        y={1}
        width={Math.max(2, x(win.t1) - x(win.t0))}
        height={H - 2}
        fill="rgb(124 242 156 / 0.08)"
        stroke="#7cf29c"
        strokeWidth={1.5}
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  );
}
