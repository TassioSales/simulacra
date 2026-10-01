import { useEffect, useMemo, useRef, useState } from "react";
import type { SeedResult } from "../lib/api";
import { nf } from "../lib/format";

const COLORS = { pending: "#182029", pass: "#2f6b43", fail: "#ff6b6b" };

/** One square per seed, drawn on a canvas so 100k seeds stay cheap. */
export function SeedGrid({
  start,
  total,
  results,
  onPick,
}: {
  start: number;
  total: number;
  results: SeedResult[];
  onPick: (seed: number) => void;
}) {
  const wrap = useRef<HTMLDivElement>(null);
  const canvas = useRef<HTMLCanvasElement>(null);
  const [width, setWidth] = useState(800);
  const [hover, setHover] = useState<{ seed: number; x: number; y: number } | null>(null);

  const cell = total <= 300 ? 16 : total <= 1500 ? 10 : total <= 6000 ? 7 : total <= 25000 ? 4 : 3;
  const gap = cell >= 7 ? 2 : 1;
  const step = cell + gap;
  const cols = Math.max(1, Math.floor((width + gap) / step));
  const rows = Math.ceil(total / cols);

  const byIndex = useMemo(() => {
    const m = new Map<number, SeedResult>();
    for (const r of results) m.set(r.seed - start, r);
    return m;
  }, [results, start]);

  useEffect(() => {
    const el = wrap.current;
    if (!el) return;
    const ro = new ResizeObserver(([e]) => setWidth(Math.floor(e!.contentRect.width)));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  useEffect(() => {
    const c = canvas.current;
    if (!c) return;
    const dpr = window.devicePixelRatio || 1;
    const h = rows * step;
    c.width = width * dpr;
    c.height = h * dpr;
    c.style.height = `${h}px`;
    const ctx = c.getContext("2d")!;
    ctx.scale(dpr, dpr);
    ctx.fillStyle = COLORS.pending;
    for (let i = 0; i < total; i++) {
      if (byIndex.has(i)) continue;
      ctx.fillRect((i % cols) * step, Math.floor(i / cols) * step, cell, cell);
    }
    ctx.fillStyle = COLORS.pass;
    for (const [i, r] of byIndex) if (!r.failed) ctx.fillRect((i % cols) * step, Math.floor(i / cols) * step, cell, cell);
    ctx.fillStyle = COLORS.fail;
    for (const [i, r] of byIndex) if (r.failed) ctx.fillRect((i % cols) * step, Math.floor(i / cols) * step, cell, cell);
  }, [byIndex, width, rows, cols, step, cell, total]);

  const indexAt = (e: React.MouseEvent) => {
    const rect = canvas.current!.getBoundingClientRect();
    const x = e.clientX - rect.left;
    const y = e.clientY - rect.top;
    const col = Math.floor(x / step);
    const row = Math.floor(y / step);
    const i = row * cols + col;
    return col < cols && i >= 0 && i < total ? { i, x, y } : null;
  };

  const hovered = hover ? byIndex.get(hover.seed - start) : undefined;

  return (
    <div ref={wrap} className="relative">
      <canvas
        ref={canvas}
        className="block w-full cursor-crosshair"
        onMouseMove={(e) => {
          const p = indexAt(e);
          setHover(p ? { seed: start + p.i, x: p.x, y: p.y } : null);
        }}
        onMouseLeave={() => setHover(null)}
        onClick={(e) => {
          const p = indexAt(e);
          if (p && byIndex.has(p.i)) onPick(start + p.i);
        }}
      />
      {hover && (
        <div
          className="pointer-events-none absolute z-10 w-max max-w-80 -translate-x-1/2 -translate-y-full rounded-lg border border-ink-600 bg-ink-800 px-3 py-2 text-xs shadow-xl"
          style={{ left: Math.min(Math.max(hover.x, 120), width - 120), top: hover.y - 8 }}
        >
          <div className="font-mono text-ink-100">seed {nf.format(hover.seed)}</div>
          {!hovered && <div className="text-ink-400">ainda não executada</div>}
          {hovered && !hovered.failed && (
            <div className="text-phos">
              passou · {hovered.ops} ops · {hovered.faults} falhas injetadas
            </div>
          )}
          {hovered?.failed && <div className="text-fail">{hovered.message}</div>}
        </div>
      )}
    </div>
  );
}
