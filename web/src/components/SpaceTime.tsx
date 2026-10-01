import { memo, useMemo, useRef } from "react";
import type { Fault, Op } from "../lib/api";
import { clientColor, fmtTime, nodeName, serverColors } from "../lib/format";
import { colorOf, tickStep, type Arrow, type Model } from "../lib/trace";
import { usePanZoom } from "../lib/usePanZoom";
import type { TimeWindow } from "../lib/useTimeWindow";

const GUTTER = 52;
const TOP = 26;
const SERVER_H = 46;
const CLIENT_H = 30;
const MAX_ARROWS = 2500;

type Props = {
  model: Model;
  faults: Fault[];
  history: Op[];
  win: TimeWindow;
  hidden: Set<string>;
  hideHeartbeats: boolean;
  selected: Arrow | null;
  onSelect: (a: Arrow | null) => void;
};

/**
 * Lamport-style space-time diagram: one lane per node, time flows right,
 * every message is an arrow from its send to its delivery (or its drop).
 */
export const SpaceTime = memo(function SpaceTime({ model, faults, history, win, hidden, hideHeartbeats, selected, onSelect }: Props) {
  const ref = useRef<HTMLDivElement>(null);
  const width = usePanZoom(ref, win, GUTTER);
  const plotW = width - GUTTER;
  const x = (t: number) => GUTTER + ((t - win.t0) / win.span) * plotW;

  const lanes = useMemo(() => {
    const out: { id: number; y: number; h: number; server: boolean }[] = [];
    let y = TOP;
    for (let i = 0; i < model.servers; i++) {
      out.push({ id: i, y: y + SERVER_H / 2, h: SERVER_H, server: true });
      y += SERVER_H;
    }
    y += 10;
    for (let i = 0; i < model.clients; i++) {
      out.push({ id: model.servers + i, y: y + CLIENT_H / 2, h: CLIENT_H, server: false });
      y += CLIENT_H;
    }
    return { list: out, height: y + 8, byId: new Map(out.map((l) => [l.id, l])) };
  }, [model.servers, model.clients]);

  const visible = useMemo(() => {
    const v: Arrow[] = [];
    for (const a of model.arrows) {
      if (a.t0 > win.t1 || a.t1 < win.t0) continue;
      if (hidden.has(a.type) || (hideHeartbeats && a.heartbeat)) continue;
      v.push(a);
    }
    return v;
  }, [model.arrows, win.t0, win.t1, hidden, hideHeartbeats]);

  const shown = visible.length > MAX_ARROWS ? visible.slice(0, MAX_ARROWS) : visible;
  const step = tickStep(win.span);
  const ticks: number[] = [];
  for (let t = Math.ceil(win.t0 / step) * step; t <= win.t1; t += step) ticks.push(t);
  const laneY = (id: number) => lanes.byId.get(id)?.y ?? 0;
  const serverBottom = TOP + model.servers * SERVER_H;
  const ops = history.filter((o) => o.call < win.t1 && (o.pending || o.return > win.t0));

  return (
    <div ref={ref} className="relative cursor-grab select-none" onClick={(e) => e.target === e.currentTarget && onSelect(null)}>
      {visible.length > MAX_ARROWS && (
        <div className="absolute top-1 right-2 z-10 rounded bg-warn/15 px-2 py-0.5 text-[11px] text-warn">
          {visible.length} mensagens na janela, mostrando {MAX_ARROWS}: aproxime o zoom
        </div>
      )}
      <svg width={width} height={lanes.height} className="block" onClick={(e) => e.target === e.currentTarget && onSelect(null)}>
        <defs>
          <pattern id="hatch" width="6" height="6" patternUnits="userSpaceOnUse" patternTransform="rotate(45)">
            <rect width="6" height="6" fill="rgb(255 107 107 / 0.08)" />
            <line x1="0" y1="0" x2="0" y2="6" stroke="rgb(255 107 107 / 0.45)" strokeWidth="2" />
          </pattern>
          <clipPath id="plot">
            <rect x={GUTTER} y={0} width={plotW} height={lanes.height} />
          </clipPath>
        </defs>

        {/* time axis */}
        {ticks.map((t) => (
          <g key={t}>
            <line x1={x(t)} x2={x(t)} y1={TOP - 4} y2={lanes.height} stroke="#1f2833" />
            <text x={x(t) + 3} y={12} className="fill-ink-500 font-mono text-[10px]">
              {fmtTime(t)}
            </text>
          </g>
        ))}

        <g clipPath="url(#plot)">
          {/* partitions: amber band over the server lanes */}
          {faults
            .filter((f) => f.kind === "partition" && !f.disabled && f.at < win.t1 && f.until > win.t0)
            .map((f) => (
              <g key={f.id}>
                <rect x={x(f.at)} y={TOP - 6} width={x(f.until) - x(f.at)} height={serverBottom - TOP + 6} fill="rgb(255 181 71 / 0.06)" />
                <rect x={x(f.at)} y={TOP - 6} width={x(f.until) - x(f.at)} height={3} fill="#ffb547" />
                <text x={Math.max(x(f.at), GUTTER) + 4} y={TOP + 5} className="fill-warn text-[10px]">
                  {f.bridge >= 0
                    ? `ponte via S${f.bridge}`
                    : `${f.groups?.[0]?.map((n) => "S" + n).join(",")} | ${f.groups?.[1]?.map((n) => "S" + n).join(",")}`}
                </text>
              </g>
            ))}

          {/* crashes */}
          {model.crashes
            .filter((c) => c.t0 < win.t1 && c.t1 > win.t0)
            .map((c, i) => {
              const lane = lanes.byId.get(c.node)!;
              return (
                <g key={i}>
                  <rect x={x(c.t0)} y={lane.y - lane.h / 2 + 4} width={Math.max(2, x(c.t1) - x(c.t0))} height={lane.h - 8} fill="url(#hatch)" rx={3} />
                  <text x={Math.max(x(c.t0), GUTTER) + 4} y={lane.y + 4} className="fill-fail text-[10px] font-medium">
                    crash
                  </text>
                </g>
              );
            })}

          {/* lanes */}
          {lanes.list.map((l) => (
            <line key={l.id} x1={GUTTER} x2={width} y1={l.y} y2={l.y} stroke={l.server ? "#2b3542" : "#1f2833"} strokeWidth={l.server ? 1.5 : 1} />
          ))}

          {/* client operations as thin bars on client lanes */}
          {ops.map((o) => {
            const l = lanes.byId.get(o.client);
            if (!l) return null;
            const end = o.pending ? win.t1 : o.return;
            const c = o.input.kind === "get" ? "#6cb6ff" : o.input.kind === "put" ? "#b69cff" : "#ffb547";
            return (
              <rect
                key={o.id}
                x={x(o.call)}
                y={l.y - 3}
                width={Math.max(2, x(end) - x(o.call))}
                height={6}
                rx={2}
                fill={c}
                opacity={o.pending ? 0.25 : 0.4}
              />
            );
          })}

          {/* messages */}
          {shown.map((a) => {
            const y0 = laneY(a.from);
            const y1 = laneY(a.to);
            const x0 = x(a.t0);
            const x1 = a.dropped && a.t1 === a.t0 ? x0 + 14 : x(a.t1);
            const ye = a.dropped ? y0 + (y1 - y0) * 0.45 : y1;
            const xe = a.dropped ? x0 + (x1 - x0) * 0.45 + (a.t1 === a.t0 ? 6 : 0) : x1;
            const color = a.dropped ? "#ff6b6b" : colorOf(a.type);
            const sel = selected?.id === a.id;
            return (
              <g key={a.id} onClick={() => onSelect(sel ? null : a)} className="cursor-pointer">
                <line x1={x0} y1={y0} x2={xe} y2={ye} stroke="transparent" strokeWidth={9} />
                <line
                  x1={x0}
                  y1={y0}
                  x2={xe}
                  y2={ye}
                  stroke={color}
                  strokeWidth={sel ? 2.5 : 1.1}
                  strokeDasharray={a.dropped ? "3 3" : undefined}
                  opacity={selected && !sel ? 0.25 : a.heartbeat ? 0.45 : 0.85}
                />
                {a.dropped ? (
                  <path d={`M${xe - 3},${ye - 3} l6,6 m0,-6 l-6,6`} stroke="#ff6b6b" strokeWidth={1.5} />
                ) : (
                  <circle cx={xe} cy={ye} r={sel ? 3 : 1.8} fill={color} opacity={selected && !sel ? 0.3 : 1} />
                )}
              </g>
            );
          })}

          {/* leaders */}
          {model.leaders
            .filter((m) => m.t >= win.t0 && m.t <= win.t1)
            .map((m, i) => {
              const y = laneY(m.node);
              return (
                <g key={i}>
                  <path d={`M${x(m.t)},${y - 7} l6,7 l-6,7 l-6,-7 z`} fill="#7cf29c" stroke="#0a0d11" strokeWidth={1.5} />
                  <text x={x(m.t) + 8} y={y - 8} className="fill-phos font-mono text-[10px] font-semibold">
                    líder {m.label}
                  </text>
                </g>
              );
            })}

          {/* the failure */}
          {model.focus >= win.t0 && model.focus <= win.t1 && model.focus > 0 && (
            <g>
              <line x1={x(model.focus)} x2={x(model.focus)} y1={TOP - 6} y2={lanes.height} stroke="#ff6b6b" strokeWidth={1.5} strokeDasharray="5 4" />
              <text x={x(model.focus) + 4} y={lanes.height - 6} className="fill-fail text-[10px] font-semibold">
                violação
              </text>
            </g>
          )}
        </g>

        {/* lane labels */}
        <rect x={0} y={0} width={GUTTER - 4} height={lanes.height} fill="#0e1217" />
        {lanes.list.map((l) => (
          <g key={l.id}>
            <circle cx={12} cy={l.y} r={4} fill={l.server ? serverColors[l.id % serverColors.length] : clientColor} />
            <text x={22} y={l.y + 4} className={l.server ? "fill-ink-100 font-mono text-[12px] font-semibold" : "fill-ink-400 font-mono text-[11px]"}>
              {nodeName(l.id, model.servers)}
            </text>
          </g>
        ))}
      </svg>
    </div>
  );
});
