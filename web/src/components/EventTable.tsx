import clsx from "clsx";
import { Crosshair, Search } from "lucide-react";
import { useDeferredValue, useMemo, useState } from "react";
import type { TraceEvent } from "../lib/api";
import { fmtTime, nf, nodeName } from "../lib/format";

const ROW = 28;
const VIEW = 560;

const kindTone: Record<string, string> = {
  send: "text-sky",
  deliver: "text-ink-300",
  drop: "text-fail",
  crash: "text-fail",
  restart: "text-phos",
  partition: "text-warn",
  heal: "text-phos",
  invoke: "text-violet",
  return: "text-violet",
  timeout: "text-warn",
  observe: "text-phos",
  timer: "text-ink-500",
  log: "text-ink-300",
};

const kindLabel: Record<string, string> = {
  send: "envio",
  deliver: "entrega",
  drop: "perda",
  crash: "crash",
  restart: "reinício",
  partition: "partição",
  heal: "cura",
  invoke: "chamada",
  return: "resposta",
  timeout: "desistência",
  observe: "observação",
  timer: "timer",
  log: "log",
};

function summary(e: TraceEvent): string {
  if (e.note && !e.data) return e.note;
  if (e.data === undefined || e.data === null) return e.note ?? "";
  const s = JSON.stringify(e.data);
  return (e.note ? e.note + " · " : "") + (s.length > 160 ? s.slice(0, 160) + "…" : s);
}

/** Every event of the run, virtualized so 50k rows scroll smoothly. */
export function EventTable({ trace, servers, onLocate }: { trace: TraceEvent[]; servers: number; onLocate: (t: number) => void }) {
  const [query, setQuery] = useState("");
  const [off, setOff] = useState<Set<string>>(() => new Set(["timer", "deliver"]));
  const [scroll, setScroll] = useState(0);
  const q = useDeferredValue(query.trim().toLowerCase());

  const counts = useMemo(() => {
    const m = new Map<string, number>();
    for (const e of trace) m.set(e.kind, (m.get(e.kind) ?? 0) + 1);
    return m;
  }, [trace]);

  const rows = useMemo(
    () =>
      trace.filter((e) => {
        if (off.has(e.kind)) return false;
        if (!q) return true;
        return `${e.kind} ${e.type ?? ""} ${nodeName(e.from, servers)} ${nodeName(e.to, servers)} ${summary(e)}`
          .toLowerCase()
          .includes(q);
      }),
    [trace, off, q, servers],
  );

  const first = Math.max(0, Math.floor(scroll / ROW) - 10);
  const last = Math.min(rows.length, Math.ceil((scroll + VIEW) / ROW) + 10);

  return (
    <div>
      <div className="flex flex-wrap items-center gap-2 border-b border-ink-700 px-5 py-3">
        <label className="relative">
          <Search className="pointer-events-none absolute top-2 left-2.5 size-3.5 text-ink-500" />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="filtrar: S1, AppendEntries, term…"
            className="h-8 w-64 rounded-lg border border-ink-700 bg-ink-900 pr-2 pl-8 text-xs text-ink-100 placeholder:text-ink-500 focus:border-phos focus:outline-none"
          />
        </label>
        {[...counts.keys()].map((k) => (
          <button
            key={k}
            onClick={() => {
              const n = new Set(off);
              if (n.has(k)) n.delete(k);
              else n.add(k);
              setOff(n);
            }}
            className={clsx(
              "rounded-md border px-2 py-1 text-[11px] transition-colors",
              off.has(k) ? "border-ink-700 text-ink-500 line-through" : "border-ink-600 bg-ink-800 text-ink-200",
            )}
          >
            {kindLabel[k] ?? k} <span className="font-mono text-ink-500">{nf.format(counts.get(k) ?? 0)}</span>
          </button>
        ))}
        <span className="ml-auto text-xs text-ink-500">{nf.format(rows.length)} eventos</span>
      </div>
      <div className="overflow-y-auto font-mono text-[11.5px]" style={{ height: VIEW }} onScroll={(e) => setScroll(e.currentTarget.scrollTop)}>
        <div style={{ height: rows.length * ROW, position: "relative" }}>
          {rows.slice(first, last).map((e, i) => (
            <div
              key={first + i}
              className="group absolute inset-x-0 flex items-center gap-3 border-b border-ink-800/80 px-5 hover:bg-ink-800/60"
              style={{ top: (first + i) * ROW, height: ROW }}
            >
              <span className="w-20 shrink-0 text-right text-ink-400 tabular">{fmtTime(e.t)}</span>
              <span className={clsx("w-20 shrink-0", kindTone[e.kind])}>{kindLabel[e.kind] ?? e.kind}</span>
              <span className="w-20 shrink-0 text-ink-200">
                {nodeName(e.from, servers)}
                {e.to >= 0 && e.to !== e.from && <span className="text-ink-500"> → {nodeName(e.to, servers)}</span>}
              </span>
              <span className="w-32 shrink-0 truncate text-ink-300">{e.type}</span>
              <span className="min-w-0 flex-1 truncate text-ink-400">{summary(e)}</span>
              <button
                onClick={() => onLocate(e.t)}
                title="ver no diagrama"
                className="invisible rounded p-1 text-ink-400 group-hover:visible hover:bg-ink-700 hover:text-phos"
              >
                <Crosshair className="size-3.5" />
              </button>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
