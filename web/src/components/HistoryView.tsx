import clsx from "clsx";
import { useMemo, useRef, useState } from "react";
import type { LinReport, Op } from "../lib/api";
import { fmtTime, nodeName, opLabel } from "../lib/format";
import { tickStep } from "../lib/trace";
import { usePanZoom } from "../lib/usePanZoom";
import type { TimeWindow } from "../lib/useTimeWindow";
import { Segmented } from "./ui";

const GUTTER = 52;
const LANE = 34;

const kindColor = { get: "#6cb6ff", put: "#b69cff", cas: "#ffb547" } as const;

/**
 * Client operations as intervals (call → return), one lane per client.
 * When linearizability fails, shows the longest order the checker found and
 * the operation that fits nowhere.
 */
export function HistoryView({
  history,
  servers,
  clients,
  win,
  lin,
}: {
  history: Op[];
  servers: number;
  clients: number;
  win: TimeWindow;
  lin?: LinReport;
}) {
  const keys = useMemo(() => [...new Set(history.map((o) => o.input.key))].sort(), [history]);
  const [key, setKey] = useState<string>(lin?.key ?? "*");
  const ref = useRef<HTMLDivElement>(null);
  const width = usePanZoom(ref, win, GUTTER);
  const plotW = width - GUTTER;
  const x = (t: number) => GUTTER + ((t - win.t0) / win.span) * plotW;

  const order = useMemo(() => new Map((lin?.linearized ?? []).map((id, i) => [id, i + 1])), [lin]);
  const stuck = useMemo(() => new Set(lin?.stuck ?? []), [lin]);
  const ops = history.filter(
    (o) => (key === "*" || o.input.key === key) && o.call <= win.t1 && (o.pending || o.return >= win.t0),
  );
  const height = clients * LANE + 30;
  const step = tickStep(win.span);
  const ticks: number[] = [];
  for (let t = Math.ceil(win.t0 / step) * step; t <= win.t1; t += step) ticks.push(t);

  return (
    <div>
      <div className="flex flex-wrap items-center gap-3 border-b border-ink-700 px-5 py-3">
        <span className="text-xs text-ink-400">Chave</span>
        <Segmented value={key} onChange={setKey} options={[{ value: "*", label: "todas" }, ...keys.map((k) => ({ value: k, label: k }))]} />
        <div className="ml-auto flex flex-wrap items-center gap-3 text-[11px] text-ink-400">
          {Object.entries(kindColor).map(([k, c]) => (
            <span key={k} className="flex items-center gap-1.5">
              <span className="inline-block h-2 w-4 rounded-sm" style={{ background: c }} />
              {k}
            </span>
          ))}
          <span className="flex items-center gap-1.5">
            <span className="inline-block h-2 w-4 rounded-sm border border-dashed border-ink-300" /> sem resposta
          </span>
        </div>
      </div>
      {lin && !lin.ok && (
        <div className="border-b border-ink-700 bg-fail/5 px-5 py-2.5 text-xs leading-relaxed text-ink-200">
          O verificador explorou <b className="font-mono">{lin.states.toLocaleString("pt-BR")}</b> estados. A melhor ordem sequencial
          encontrada para a chave <code className="font-mono text-fail">{lin.key}</code> tem{" "}
          <b className="text-phos">{lin.linearized?.length ?? 0}</b> operações (numeradas); a operação em{" "}
          <span className="text-fail">vermelho</span> não cabe em nenhuma posição depois delas.
        </div>
      )}
      <div ref={ref} className="cursor-grab select-none">
        <svg width={width} height={height} className="block">
          {ticks.map((t) => (
            <g key={t}>
              <line x1={x(t)} x2={x(t)} y1={16} y2={height} stroke="#1f2833" />
              <text x={x(t) + 3} y={11} className="fill-ink-500 font-mono text-[10px]">
                {fmtTime(t)}
              </text>
            </g>
          ))}
          <clipPath id="hist-plot">
            <rect x={GUTTER} y={0} width={plotW} height={height} />
          </clipPath>
          <g clipPath="url(#hist-plot)">
            {ops.map((o) => {
              const lane = o.client - servers;
              const y = 22 + lane * LANE;
              const x0 = x(o.call);
              const x1 = o.pending ? x(win.t1) + 10 : x(o.return);
              const w = Math.max(3, x1 - x0);
              const n = order.get(o.id);
              const bad = stuck.has(o.id);
              const c = kindColor[o.input.kind];
              const label = opLabel(o.input, o.output, o.pending);
              return (
                <g key={o.id}>
                  <title>
                    #{o.id} {label} · {fmtTime(o.call)} → {o.pending ? "sem resposta" : fmtTime(o.return)}
                  </title>
                  <rect
                    x={x0}
                    y={y}
                    width={w}
                    height={LANE - 10}
                    rx={4}
                    fill={bad ? "rgb(255 107 107 / 0.22)" : `${c}26`}
                    stroke={bad ? "#ff6b6b" : n ? "#7cf29c" : c}
                    strokeWidth={bad || n ? 1.6 : 1}
                    strokeDasharray={o.pending ? "4 3" : undefined}
                  />
                  {w > 46 && (
                    <text x={x0 + 6} y={y + 16} className={clsx("font-mono text-[10.5px]", bad ? "fill-fail" : "fill-ink-100")}>
                      {label.length * 6.3 > w - 10 ? label.slice(0, Math.max(3, Math.floor((w - 16) / 6.3))) + "…" : label}
                    </text>
                  )}
                  {n !== undefined && (
                    <g>
                      <circle cx={x0} cy={y} r={7} fill="#0a0d11" stroke="#7cf29c" />
                      <text x={x0} y={y + 3} textAnchor="middle" className="fill-phos font-mono text-[8.5px] font-bold">
                        {n}
                      </text>
                    </g>
                  )}
                </g>
              );
            })}
          </g>
          <rect x={0} y={0} width={GUTTER - 4} height={height} fill="#0e1217" />
          {Array.from({ length: clients }, (_, i) => (
            <text key={i} x={12} y={22 + i * LANE + 16} className="fill-ink-300 font-mono text-[11px]">
              {nodeName(servers + i, servers)}
            </text>
          ))}
        </svg>
      </div>
    </div>
  );
}
