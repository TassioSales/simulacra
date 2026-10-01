// Types mirror the Go structs in internal/harness, internal/campaign and
// internal/sim. Times coming from the simulator are virtual microseconds.

export type RunConfig = {
  system: string;
  bugs: string[];
  servers: number;
  clients: number;
  durationMs: number;
  dropRate: number;
  dupRate: number;
  minLatencyMs: number;
  maxLatencyMs: number;
  slowRate: number;
  crashRate: number;
  partitionRate: number;
};

export type BugInfo = {
  id: string;
  title: string;
  description: string;
  detects: string;
  hint?: string;
};

export type System = {
  name: string;
  title: string;
  description: string;
  bugs: BugInfo[];
};

export type Summary = {
  id: string;
  createdAt: string;
  finishedAt?: string;
  config: RunConfig;
  startSeed: number;
  seeds: number;
  status: "running" | "done" | "cancelled" | "interrupted";
  done: number;
  failed: number;
  elapsedMs: number;
  kinds: Record<string, number>;
  firstFail?: number;
};

export type SeedResult = {
  seed: number;
  hash: string;
  failed: boolean;
  kinds?: string[];
  message?: string;
  ops: number;
  messages: number;
  faults: number;
  elapsedMs: number;
};

export type Campaign = Summary & { results: SeedResult[] };

export type Violation = {
  kind: string;
  message: string;
  details?: unknown;
};

export type LinReport = {
  ok: boolean;
  unknown: boolean;
  key?: string;
  ops?: number[];
  linearized?: number[];
  stuck?: number[];
  states: number;
};

export type Outcome = {
  seed: number;
  hash: string;
  violations: Violation[];
  ops: number;
  messages: number;
  dropped: number;
  steps: number;
  faults: number;
  aborted: boolean;
  elapsedMs: number;
};

export type Fault = {
  id: number;
  kind: "crash" | "partition";
  at: number;
  until: number;
  node: number;
  groups?: number[][];
  bridge: number;
  disabled: boolean;
  label: string;
};

export type TraceEvent = {
  t: number;
  kind:
    | "send"
    | "deliver"
    | "drop"
    | "timer"
    | "crash"
    | "restart"
    | "partition"
    | "heal"
    | "invoke"
    | "return"
    | "timeout"
    | "observe"
    | "log";
  from: number;
  to: number;
  msg?: number;
  type?: string;
  data?: unknown;
  note?: string;
};

export type KVInput = { kind: "get" | "put" | "cas"; key: string; value?: string; expected?: string };
export type KVOutput = { ok: boolean; value?: string };

export type Op = {
  id: number;
  client: number;
  call: number;
  return: number;
  input: KVInput;
  output?: KVOutput;
  pending: boolean;
};

export type Replay = {
  outcome: Outcome;
  config: RunConfig;
  faults: Fault[];
  trace: TraceEvent[];
  history: Op[];
  expectedHash?: string;
  disabled: number[];
};

export type ShrinkResult = {
  seed: number;
  before: number;
  after: number;
  disabled: number[];
  kept: number[];
  runs: number;
  violation: string;
  reproduces: boolean;
};

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    ...init,
    headers: { "Content-Type": "application/json", ...init?.headers },
  });
  if (!res.ok) {
    let msg = res.statusText;
    try {
      msg = ((await res.json()) as { error?: string }).error ?? msg;
    } catch {
      /* not JSON */
    }
    throw new ApiError(msg, res.status);
  }
  if (res.status === 204 || res.status === 202) return undefined as T;
  return (await res.json()) as T;
}

export const api = {
  systems: () => request<System[]>("/api/systems"),
  defaults: () => request<RunConfig>("/api/defaults"),
  campaigns: () => request<Summary[]>("/api/campaigns"),
  campaign: (id: string) => request<Campaign>(`/api/campaigns/${id}`),
  create: (body: { config: RunConfig; seeds: number; startSeed: number }) =>
    request<Summary>("/api/campaigns", { method: "POST", body: JSON.stringify(body) }),
  cancel: (id: string) => request<void>(`/api/campaigns/${id}/cancel`, { method: "POST" }),
  remove: (id: string) => request<void>(`/api/campaigns/${id}`, { method: "DELETE" }),
  replay: (id: string, seed: number, disabled: number[]) =>
    request<Replay>(
      `/api/campaigns/${id}/seeds/${seed}` + (disabled.length ? `?disabled=${disabled.join(",")}` : ""),
    ),
  shrink: (id: string, seed: number) =>
    request<ShrinkResult>(`/api/campaigns/${id}/seeds/${seed}/shrink`, { method: "POST" }),
};

export const violationLabel: Record<string, string> = {
  linearizability: "Linearizabilidade",
  "election-safety": "Segurança de eleição",
  "state-machine-safety": "Segurança da máquina de estados",
};

export const violationBlurb: Record<string, string> = {
  linearizability: "Nenhuma ordem sequencial das operações explica as respostas que os clientes receberam.",
  "election-safety": "Dois servidores se declararam líder no mesmo termo.",
  "state-machine-safety": "Dois servidores aplicaram comandos diferentes na mesma posição do log.",
};
