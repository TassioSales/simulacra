import type { KVInput, KVOutput, Op } from "./api";

/** Virtual microseconds → human string. */
export function fmtTime(us: number): string {
  const ms = us / 1000;
  if (Math.abs(ms) >= 1000) return `${(ms / 1000).toFixed(ms >= 10000 ? 1 : 2)}s`;
  if (Math.abs(ms) >= 10) return `${ms.toFixed(0)}ms`;
  return `${ms.toFixed(2)}ms`;
}

export function fmtDuration(ms: number): string {
  if (ms < 1000) return `${ms.toFixed(0)} ms`;
  const s = ms / 1000;
  if (s < 60) return `${s.toFixed(1)} s`;
  return `${Math.floor(s / 60)} min ${Math.round(s % 60)} s`;
}

export const nf = new Intl.NumberFormat("pt-BR");
export const pct = (v: number) => `${(v * 100).toLocaleString("pt-BR", { maximumFractionDigits: 1 })}%`;

export function relTime(iso: string): string {
  const d = (Date.now() - new Date(iso).getTime()) / 1000;
  if (d < 60) return "agora";
  if (d < 3600) return `há ${Math.floor(d / 60)} min`;
  if (d < 86400) return `há ${Math.floor(d / 3600)} h`;
  return new Date(iso).toLocaleDateString("pt-BR");
}

export const nodeName = (id: number, servers: number) => (id < 0 ? "—" : id < servers ? `S${id}` : `C${id}`);

export function opLabel(input: KVInput, output?: KVOutput, pending?: boolean): string {
  const nil = (v?: string) => (v ? v : "∅");
  let base: string;
  switch (input.kind) {
    case "put":
      base = `put ${input.key}=${input.value}`;
      break;
    case "cas":
      base = `cas ${input.key} ${nil(input.expected)}→${input.value}`;
      break;
    default:
      base = `get ${input.key}`;
  }
  if (pending) return `${base} ⋯ ?`;
  if (!output) return base;
  if (input.kind === "get") return `${base} → ${nil(output.value)}`;
  if (input.kind === "cas") return `${base} ${output.ok ? "✓" : "✗"}`;
  return base;
}

export const opEnd = (op: Op, end: number) => (op.pending ? end : op.return);

export const serverColors = ["#7cf29c", "#6cb6ff", "#b69cff", "#ffb547", "#ff8fb1", "#5ee1d9", "#e8e27a"];
export const clientColor = "#95a1ae";
