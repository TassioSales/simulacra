import { useMutation, useQuery, type UseMutationResult } from "@tanstack/react-query";
import clsx from "clsx";
import { ArrowLeft, CheckCircle2, Fingerprint, Minimize2, RotateCcw, XCircle, ZoomIn, ZoomOut } from "lucide-react";
import { useMemo, useState } from "react";
import { Link, useParams, useSearchParams } from "react-router";
import { EventTable } from "../components/EventTable";
import { HistoryView } from "../components/HistoryView";
import { Overview } from "../components/Overview";
import { SpaceTime } from "../components/SpaceTime";
import { Badge, Button, Card, CardHeader, ErrorBox, Kbd, Segmented, Spinner, Stat } from "../components/ui";
import { api, violationBlurb, violationLabel, type LinReport, type Replay, type ShrinkResult } from "../lib/api";
import { fmtTime, nf, nodeName } from "../lib/format";
import { buildModel, colorOf, type Arrow } from "../lib/trace";
import { useTimeWindow } from "../lib/useTimeWindow";

type Tab = "diagram" | "history" | "events";

export function RunPage() {
  const { id = "", seed: seedParam = "0" } = useParams();
  const seed = Number(seedParam);
  const [params, setParams] = useSearchParams();
  const disabled = useMemo(
    () =>
      (params.get("disabled") ?? "")
        .split(",")
        .filter(Boolean)
        .map(Number)
        .filter((n) => Number.isInteger(n) && n >= 0)
        .sort((a, b) => a - b),
    [params],
  );
  const q = useQuery({
    queryKey: ["replay", id, seed, disabled.join(",")],
    queryFn: () => api.replay(id, seed, disabled),
    placeholderData: (prev) => prev,
  });

  const setDisabled = (d: number[]) => setParams(d.length ? { disabled: d.join(",") } : {}, { replace: true });
  // Lives here, above the keyed RunView, so its result survives the re-simulation.
  const shrink = useMutation({
    mutationFn: () => api.shrink(id, seed),
    onSuccess: (r) => setDisabled(r.disabled),
  });

  if (q.isPending) return <Spinner label={`Re-simulando a seed ${seed}…`} />;
  if (q.isError) return <ErrorBox error={q.error} />;
  return (
    <RunView
      key={`${seed}:${disabled.join(",")}`}
      replay={q.data}
      id={id}
      seed={seed}
      disabled={disabled}
      refetching={q.isFetching}
      setDisabled={setDisabled}
      shrink={shrink}
    />
  );
}

function RunView({
  replay,
  id,
  seed,
  disabled,
  refetching,
  setDisabled,
  shrink,
}: {
  replay: Replay;
  id: string;
  seed: number;
  disabled: number[];
  refetching: boolean;
  setDisabled: (d: number[]) => void;
  shrink: UseMutationResult<ShrinkResult, Error, void>;
}) {
  const model = useMemo(() => buildModel(replay), [replay]);
  const o = replay.outcome;
  const failed = o.violations.length > 0;
  const win = useTimeWindow(model.duration, failed ? model.focus : 300_000, failed ? 120_000 : 600_000);
  const [tab, setTab] = useState<Tab>("diagram");
  const [hidden, setHidden] = useState<Set<string>>(new Set());
  const [hideHeartbeats, setHideHeartbeats] = useState(true);
  const [selected, setSelected] = useState<Arrow | null>(null);
  const lin = o.violations.find((v) => v.kind === "linearizability")?.details as LinReport | undefined;
  const deterministic = replay.expectedHash ? replay.expectedHash === o.hash : undefined;
  const active = replay.faults.filter((f) => !f.disabled);

  const locate = (t: number) => {
    win.center(t);
    setTab("diagram");
  };

  return (
    <div className={clsx("space-y-5 transition-opacity", refetching && "opacity-60")}>
      <div className="flex flex-wrap items-center gap-3">
        <Link
          to={`/campaigns/${id}`}
          className="rounded-md p-1.5 text-ink-400 hover:bg-ink-800 hover:text-ink-100"
          aria-label="Voltar para a campanha"
        >
          <ArrowLeft className="size-4" />
        </Link>
        <h1 className="font-mono text-lg font-semibold">seed {nf.format(seed)}</h1>
        {failed ? (
          <Badge tone="fail">
            <XCircle className="size-3" /> violou {o.violations.length} propriedade{o.violations.length > 1 ? "s" : ""}
          </Badge>
        ) : (
          <Badge tone="pass">
            <CheckCircle2 className="size-3" /> todas as propriedades valem
          </Badge>
        )}
        {deterministic === true && (
          <Badge tone="pass" className="font-mono">
            <Fingerprint className="size-3" /> replay idêntico à campanha · {o.hash}
          </Badge>
        )}
        {deterministic === false && <Badge tone="fail">hash diverge da campanha: não determinístico!</Badge>}
        {disabled.length > 0 && (
          <Badge tone="warn">
            {disabled.length} de {replay.faults.length} falhas desligadas
          </Badge>
        )}
        <div className="ml-auto flex gap-2">
          {disabled.length > 0 && (
            <Button size="sm" variant="ghost" icon={<RotateCcw className="size-3.5" />} onClick={() => setDisabled([])}>
              Todas as falhas
            </Button>
          )}
          {failed && (
            <Button size="sm" icon={<Minimize2 className="size-3.5" />} loading={shrink.isPending} onClick={() => shrink.mutate()}>
              Minimizar falhas
            </Button>
          )}
        </div>
      </div>

      {shrink.data && (
        <div className="rounded-xl border border-phos/30 bg-phos/5 px-5 py-3 text-sm">
          Minimização concluída em <b>{shrink.data.runs}</b> execuções: de <b>{shrink.data.before}</b> falhas injetadas para{" "}
          <b className="text-phos">{shrink.data.after}</b> ainda reproduzindo <b>{violationLabel[shrink.data.violation]}</b>.
        </div>
      )}
      {shrink.isError && <ErrorBox error={shrink.error} />}

      {failed && (
        <div className="grid gap-3 md:grid-cols-2">
          {o.violations.map((v, i) => (
            <div key={i} className="rounded-xl border border-fail/40 bg-fail/5 px-5 py-4">
              <div className="flex items-center gap-2 text-sm font-semibold text-fail">
                <XCircle className="size-4" /> {violationLabel[v.kind] ?? v.kind}
              </div>
              <p className="mt-1 text-xs text-ink-400">{violationBlurb[v.kind]}</p>
              <p className="mt-2 font-mono text-xs leading-relaxed text-ink-100">{v.message}</p>
            </div>
          ))}
        </div>
      )}

      <div className="grid grid-cols-2 gap-3 md:grid-cols-6">
        <Stat label="Operações" value={nf.format(o.ops)} />
        <Stat label="Mensagens" value={nf.format(o.messages)} />
        <Stat label="Perdidas" value={nf.format(o.dropped)} />
        <Stat label="Falhas ativas" value={active.length} tone={active.length ? "warn" : undefined} />
        <Stat label="Eventos" value={nf.format(o.steps)} />
        <Stat label="Re-simulado em" value={`${o.elapsedMs.toFixed(0)} ms`} sub={`${replay.config.durationMs / 1000} s virtuais`} />
      </div>

      <div className="grid items-start gap-5 xl:grid-cols-[1fr_340px]">
        <Card className="min-w-0 overflow-hidden">
          <div className="flex flex-wrap items-center gap-3 border-b border-ink-700 px-5 py-3">
            <Segmented<Tab>
              value={tab}
              onChange={setTab}
              options={[
                { value: "diagram", label: "Diagrama espaço-tempo" },
                { value: "history", label: "Histórico dos clientes" },
                { value: "events", label: "Eventos" },
              ]}
            />
            {tab !== "events" && (
              <div className="ml-auto flex items-center gap-2 text-xs text-ink-400">
                <span className="font-mono tabular">
                  {fmtTime(win.t0)} – {fmtTime(win.t1)}
                </span>
                <Button size="sm" variant="ghost" aria-label="Afastar" onClick={() => win.zoom(2)}>
                  <ZoomOut className="size-3.5" />
                </Button>
                <Button size="sm" variant="ghost" aria-label="Aproximar" onClick={() => win.zoom(0.5)}>
                  <ZoomIn className="size-3.5" />
                </Button>
                {failed && (
                  <Button size="sm" variant="ghost" onClick={() => win.show(model.focus - 60_000, model.focus + 60_000)}>
                    ir à violação
                  </Button>
                )}
              </div>
            )}
          </div>

          {tab !== "events" && (
            <div className="space-y-2 border-b border-ink-700 px-5 py-3">
              <Overview model={model} faults={replay.faults} win={win} />
              <p className="text-[11px] text-ink-500">
                <Kbd>roda</Kbd> zoom · <Kbd>arrastar</Kbd> navegar · <Kbd>shift + roda</Kbd> rolar no tempo · clique numa mensagem para
                ver o conteúdo
              </p>
            </div>
          )}

          {tab === "diagram" && (
            <>
              <div className="flex flex-wrap items-center gap-2 border-b border-ink-700 px-5 py-2.5">
                {model.types.map((t) => (
                  <button
                    key={t}
                    onClick={() => {
                      const n = new Set(hidden);
                      if (n.has(t)) n.delete(t);
                      else n.add(t);
                      setHidden(n);
                    }}
                    className={clsx(
                      "flex items-center gap-1.5 rounded-md border px-2 py-1 text-[11px]",
                      hidden.has(t) ? "border-ink-700 text-ink-500 line-through" : "border-ink-600 bg-ink-800 text-ink-200",
                    )}
                  >
                    <span className="inline-block h-0.5 w-3" style={{ background: colorOf(t) }} />
                    {t}
                  </button>
                ))}
                <label className="ml-auto flex items-center gap-2 text-xs text-ink-300">
                  <input type="checkbox" checked={hideHeartbeats} onChange={(e) => setHideHeartbeats(e.target.checked)} className="accent-[var(--color-phos)]" />
                  ocultar heartbeats
                </label>
              </div>
              <SpaceTime
                model={model}
                faults={replay.faults}
                history={replay.history}
                win={win}
                hidden={hidden}
                hideHeartbeats={hideHeartbeats}
                selected={selected}
                onSelect={setSelected}
              />
              <MessageDetails arrow={selected} servers={model.servers} />
            </>
          )}
          {tab === "history" && (
            <HistoryView history={replay.history} servers={model.servers} clients={model.clients} win={win} lin={lin} />
          )}
          {tab === "events" && <EventTable trace={replay.trace} servers={model.servers} onLocate={locate} />}
        </Card>

        <Card>
          <CardHeader
            title="Falhas injetadas"
            hint="Desmarque para re-simular sem ela. A seed continua a mesma."
          />
          {replay.faults.length === 0 ? (
            <p className="px-5 py-4 text-sm text-ink-400">Nenhuma falha planejada para esta seed.</p>
          ) : (
            <ul className="max-h-[640px] divide-y divide-ink-800 overflow-y-auto">
              {replay.faults.map((f) => (
                <li key={f.id} className="flex items-start gap-3 px-5 py-2.5">
                  <input
                    type="checkbox"
                    aria-label={`Ativar falha ${f.id}`}
                    className="mt-1 accent-[var(--color-warn)]"
                    checked={!f.disabled}
                    onChange={() =>
                      setDisabled(f.disabled ? disabled.filter((d) => d !== f.id) : [...disabled, f.id].sort((a, b) => a - b))
                    }
                  />
                  <button className="min-w-0 flex-1 text-left" onClick={() => win.show(f.at - 50_000, f.until + 50_000)}>
                    <div className={clsx("flex items-center gap-2 text-xs", f.disabled ? "text-ink-500 line-through" : "text-ink-100")}>
                      <span className={clsx("size-2 rounded-full", f.kind === "crash" ? "bg-fail" : "bg-warn")} />
                      <span className="font-medium">
                        {f.kind === "crash" ? `crash ${nodeName(f.node, model.servers)}` : f.bridge >= 0 ? "partição em ponte" : "partição"}
                      </span>
                      <span className="font-mono text-ink-500">#{f.id}</span>
                    </div>
                    <div className="mt-0.5 font-mono text-[11px] text-ink-400">
                      {fmtTime(f.at)} → {fmtTime(f.until)}
                      {f.kind === "partition" && (
                        <span className="text-ink-500">
                          {" "}
                          · {f.groups?.[0]?.map((n) => "S" + n).join(",")} ✕ {f.groups?.[1]?.map((n) => "S" + n).join(",")}
                          {f.bridge >= 0 && ` (S${f.bridge} liga os dois)`}
                        </span>
                      )}
                    </div>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </Card>
      </div>
    </div>
  );
}

function MessageDetails({ arrow, servers }: { arrow: Arrow | null; servers: number }) {
  if (!arrow) {
    return <div className="border-t border-ink-700 px-5 py-3 text-xs text-ink-500">Nenhuma mensagem selecionada.</div>;
  }
  return (
    <div className="grid gap-4 border-t border-ink-700 px-5 py-4 md:grid-cols-[240px_1fr]">
      <dl className="space-y-1.5 text-xs">
        <div className="flex items-center gap-2">
          <span className="inline-block h-0.5 w-4" style={{ background: colorOf(arrow.type) }} />
          <span className="font-mono font-semibold">{arrow.type}</span>
        </div>
        <div className="flex justify-between">
          <dt className="text-ink-400">rota</dt>
          <dd className="font-mono">
            {nodeName(arrow.from, servers)} → {nodeName(arrow.to, servers)}
          </dd>
        </div>
        <div className="flex justify-between">
          <dt className="text-ink-400">enviada</dt>
          <dd className="font-mono">{fmtTime(arrow.t0)}</dd>
        </div>
        <div className="flex justify-between">
          <dt className="text-ink-400">{arrow.dropped ? "perdida" : "entregue"}</dt>
          <dd className={clsx("font-mono", arrow.dropped && "text-fail")}>{fmtTime(arrow.t1)}</dd>
        </div>
        {!arrow.dropped && (
          <div className="flex justify-between">
            <dt className="text-ink-400">latência</dt>
            <dd className="font-mono">{fmtTime(arrow.t1 - arrow.t0)}</dd>
          </div>
        )}
        {arrow.note && <div className="text-fail">{arrow.note}</div>}
      </dl>
      <pre className="max-h-60 overflow-auto rounded-lg bg-ink-900 p-3 font-mono text-[11px] leading-relaxed text-ink-200">
        {JSON.stringify(arrow.data, null, 2)}
      </pre>
    </div>
  );
}
