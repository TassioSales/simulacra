import type { LinReport, Replay, TraceEvent } from "./api";

export type Arrow = {
  id: number;
  from: number;
  to: number;
  t0: number;
  t1: number;
  type: string;
  data: unknown;
  dropped: boolean;
  note?: string;
  heartbeat: boolean;
};

export type Interval = { node: number; t0: number; t1: number };
export type Mark = { node: number; t: number; label: string };

export type Model = {
  duration: number;
  servers: number;
  clients: number;
  arrows: Arrow[];
  crashes: Interval[];
  leaders: Mark[];
  density: number[];
  types: string[];
  focus: number;
};

export const typeColors: Record<string, string> = {
  RequestVote: "#ffb547",
  VoteReply: "#d9a85c",
  AppendEntries: "#6cb6ff",
  AppendReply: "#4d82b8",
  ClientRequest: "#b69cff",
  ClientReply: "#8a74d1",
};
export const colorOf = (type: string) => typeColors[type] ?? "#95a1ae";

const BINS = 240;

export function buildModel(r: Replay): Model {
  const duration = r.config.durationMs * 1000;
  const sends = new Map<number, TraceEvent>();
  const arrows: Arrow[] = [];
  const crashes: Interval[] = [];
  const openCrash = new Map<number, number>();
  const leaders: Mark[] = [];
  const density = new Array<number>(BINS).fill(0);
  const types = new Set<string>();
  const lastMatch = new Map<string, number>();

  for (const e of r.trace) {
    switch (e.kind) {
      case "send": {
        sends.set(e.msg!, e);
        const b = Math.min(BINS - 1, Math.floor((e.t / duration) * BINS));
        density[b] = (density[b] ?? 0) + 1;
        break;
      }
      case "deliver":
      case "drop": {
        const s = sends.get(e.msg!);
        if (!s) break;
        const type = e.type ?? "?";
        types.add(type);
        const data = s.data as Record<string, unknown> | undefined;
        let heartbeat = false;
        if (type === "AppendEntries") {
          heartbeat = !(Array.isArray(data?.entries) && data.entries.length > 0);
        } else if (type === "AppendReply" && e.kind === "deliver") {
          const key = `${e.from}>${e.to}`;
          const match = Number(data?.match ?? -1);
          heartbeat = data?.success === true && lastMatch.get(key) === match;
          lastMatch.set(key, match);
        }
        arrows.push({
          id: arrows.length,
          from: e.from,
          to: e.to,
          t0: s.t,
          t1: e.t,
          type,
          data: s.data,
          dropped: e.kind === "drop",
          note: e.note,
          heartbeat,
        });
        break;
      }
      case "crash":
        openCrash.set(e.from, e.t);
        break;
      case "restart": {
        const t0 = openCrash.get(e.from);
        if (t0 !== undefined) crashes.push({ node: e.from, t0, t1: e.t });
        openCrash.delete(e.from);
        break;
      }
      case "observe":
        if (e.type === "leader") {
          const term = (e.data as { term?: number } | undefined)?.term;
          leaders.push({ node: e.from, t: e.t, label: `T${term}` });
        }
        break;
    }
  }
  for (const [node, t0] of openCrash) crashes.push({ node, t0, t1: duration });

  const order = ["RequestVote", "VoteReply", "AppendEntries", "AppendReply", "ClientRequest", "ClientReply"];
  return {
    duration,
    servers: r.config.servers,
    clients: r.config.clients,
    arrows,
    crashes,
    leaders,
    density,
    types: [...types].sort((a, b) => rank(order, a) - rank(order, b)),
    focus: focusOf(r),
  };
}

const rank = (order: string[], t: string) => (order.includes(t) ? order.indexOf(t) : order.length);

/** Where the interesting part of a failing run is. */
export function focusOf(r: Replay): number {
  const v = r.outcome.violations[0];
  if (!v) return 0;
  const d = v.details as Record<string, unknown> | undefined;
  if (v.kind === "linearizability") {
    const lin = d as unknown as LinReport;
    const id = lin.stuck?.[0] ?? lin.ops?.[0];
    const op = r.history.find((o) => o.id === id);
    return op?.call ?? 0;
  }
  return typeof d?.t === "number" ? d.t : 0;
}

/** Nice tick spacing (in µs) for a span. */
export function tickStep(span: number, target = 8): number {
  const raw = span / target;
  const pow = 10 ** Math.floor(Math.log10(raw));
  for (const m of [1, 2, 5, 10]) if (raw <= m * pow) return m * pow;
  return 10 * pow;
}
