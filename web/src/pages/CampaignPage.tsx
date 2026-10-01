import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, ArrowRight, Square, Trash2 } from "lucide-react";
import { useEffect, useMemo, useRef } from "react";
import { Link, useNavigate, useParams } from "react-router";
import { SeedGrid } from "../components/SeedGrid";
import { StatusBadge } from "../components/StatusBadge";
import { Badge, Button, Card, CardHeader, ErrorBox, Spinner, Stat } from "../components/ui";
import { api, violationBlurb, violationLabel, type Campaign, type SeedResult, type Summary } from "../lib/api";
import { fmtDuration, nf, pct } from "../lib/format";

/** Loads a campaign and, while it runs, follows its SSE progress stream. */
function useLiveCampaign(id: string) {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["campaign", id], queryFn: () => api.campaign(id) });
  const running = q.data?.status === "running";
  const cursor = useRef(0);
  cursor.current = q.data?.results.length ?? 0;

  useEffect(() => {
    if (!running) return;
    const es = new EventSource(`/api/campaigns/${id}/stream?cursor=${cursor.current}`);
    es.addEventListener("progress", (ev) => {
      const p = JSON.parse((ev as MessageEvent).data) as { summary: Summary; results: SeedResult[] };
      qc.setQueryData<Campaign>(["campaign", id], (prev) =>
        prev ? { ...prev, ...p.summary, results: prev.results.concat(p.results) } : prev,
      );
    });
    es.addEventListener("end", () => {
      es.close();
      qc.invalidateQueries({ queryKey: ["campaigns"] });
    });
    es.onerror = () => es.close();
    return () => es.close();
  }, [id, running, qc]);

  return q;
}

export function CampaignPage() {
  const { id = "" } = useParams();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const q = useLiveCampaign(id);
  const cancel = useMutation({ mutationFn: () => api.cancel(id) });
  const remove = useMutation({
    mutationFn: () => api.remove(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["campaigns"] });
      navigate("/");
    },
  });

  const failures = useMemo(
    () => (q.data?.results ?? []).filter((r) => r.failed).sort((a, b) => a.seed - b.seed),
    [q.data?.results],
  );

  if (q.isPending) return <Spinner label="Carregando campanha…" />;
  if (q.isError) return <ErrorBox error={q.error} />;
  const c = q.data;
  const rate = c.elapsedMs > 0 ? (c.done / c.elapsedMs) * 1000 : 0;
  const avgOps = c.results.length ? c.results.reduce((s, r) => s + r.ops, 0) / c.results.length : 0;
  const avgFaults = c.results.length ? c.results.reduce((s, r) => s + r.faults, 0) / c.results.length : 0;
  const open = (seed: number) => navigate(`/campaigns/${id}/seeds/${seed}`);

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center gap-3">
        <Link to="/" className="rounded-md p-1.5 text-ink-400 hover:bg-ink-800 hover:text-ink-100" aria-label="Voltar">
          <ArrowLeft className="size-4" />
        </Link>
        <h1 className="font-mono text-lg font-semibold">{c.id}</h1>
        <StatusBadge status={c.status} failed={c.failed} />
        <div className="flex flex-wrap gap-1.5">
          <Badge>{c.config.system}</Badge>
          {c.config.bugs.length === 0 && <Badge tone="pass">implementação correta</Badge>}
          {c.config.bugs.map((b) => (
            <Badge key={b} tone="fail">
              {b}
            </Badge>
          ))}
        </div>
        <div className="ml-auto flex gap-2">
          {c.status === "running" && (
            <Button size="sm" onClick={() => cancel.mutate()} loading={cancel.isPending} icon={<Square className="size-3.5" />}>
              Parar
            </Button>
          )}
          <Button
            size="sm"
            variant="danger"
            icon={<Trash2 className="size-3.5" />}
            loading={remove.isPending}
            onClick={() => confirm("Apagar esta campanha?") && remove.mutate()}
          >
            Apagar
          </Button>
        </div>
      </div>

      <div className="grid grid-cols-2 gap-3 md:grid-cols-5">
        <Stat label="Seeds" value={`${nf.format(c.done)}`} sub={`de ${nf.format(c.seeds)} · a partir de ${nf.format(c.startSeed)}`} />
        <Stat
          label="Falharam"
          value={nf.format(c.failed)}
          tone={c.failed ? "fail" : "pass"}
          sub={c.done ? `${pct(c.failed / c.done)} das seeds` : "—"}
        />
        <Stat label="Velocidade" value={nf.format(Math.round(rate))} sub="seeds por segundo" />
        <Stat label="Tempo" value={fmtDuration(c.elapsedMs)} sub={`${nf.format(Math.round(avgOps))} ops/seed · ${avgFaults.toFixed(1)} falhas/seed`} />
        <Stat
          label="Primeira falha"
          value={c.firstFail !== undefined ? nf.format(c.firstFail) : "—"}
          tone={c.firstFail !== undefined ? "fail" : undefined}
          sub={
            c.firstFail !== undefined ? (
              <button className="text-fail hover:underline" onClick={() => open(c.firstFail!)}>
                abrir replay →
              </button>
            ) : (
              "nenhuma violação"
            )
          }
        />
      </div>

      <div className="h-1.5 overflow-hidden rounded-full bg-ink-800">
        <div
          className="h-full rounded-full bg-phos transition-[width] duration-300"
          style={{ width: `${(c.done / c.seeds) * 100}%` }}
        />
      </div>

      <div className="grid items-start gap-6 xl:grid-cols-[1fr_420px]">
        <Card>
          <CardHeader
            title="Mapa de seeds"
            hint="Verde: passou. Vermelho: violou uma propriedade. Clique para abrir o replay."
          />
          <div className="p-5">
            <SeedGrid start={c.startSeed} total={c.seeds} results={c.results} onPick={open} />
          </div>
        </Card>

        <div className="space-y-6">
          <Card>
            <CardHeader title="Violações por tipo" />
            <div className="space-y-3 p-5">
              {Object.keys(c.kinds).length === 0 && (
                <p className="text-sm text-ink-400">
                  {c.status === "running" ? "Nenhuma até agora." : "Nenhuma propriedade foi violada nesta campanha."}
                </p>
              )}
              {Object.entries(c.kinds).map(([k, n]) => (
                <div key={k}>
                  <div className="flex justify-between text-sm">
                    <span>{violationLabel[k] ?? k}</span>
                    <span className="font-mono text-fail tabular">{nf.format(n)}</span>
                  </div>
                  <div className="mt-1 h-1 rounded-full bg-ink-800">
                    <div className="h-full rounded-full bg-fail" style={{ width: `${(n / Math.max(1, c.failed)) * 100}%` }} />
                  </div>
                  <p className="mt-1 text-xs text-ink-400">{violationBlurb[k]}</p>
                </div>
              ))}
            </div>
          </Card>

          <Card>
            <CardHeader title="Seeds que falharam" hint={failures.length > 100 ? "mostrando as 100 menores" : undefined} />
            {failures.length === 0 ? (
              <p className="px-5 py-4 text-sm text-ink-400">—</p>
            ) : (
              <ul className="max-h-[420px] divide-y divide-ink-700/70 overflow-y-auto">
                {failures.slice(0, 100).map((r) => (
                  <li key={r.seed}>
                    <Link
                      to={`/campaigns/${id}/seeds/${r.seed}`}
                      className="group flex items-start gap-3 px-5 py-2.5 hover:bg-ink-800/60"
                    >
                      <span className="w-20 shrink-0 font-mono text-sm text-fail tabular">{r.seed}</span>
                      <span className="min-w-0 flex-1 text-xs leading-relaxed text-ink-300">{r.message}</span>
                      <ArrowRight className="mt-0.5 size-3.5 shrink-0 text-ink-600 group-hover:text-ink-300" />
                    </Link>
                  </li>
                ))}
              </ul>
            )}
          </Card>

          <Card>
            <CardHeader title="Cenário" />
            <dl className="grid grid-cols-2 gap-x-4 gap-y-2 p-5 text-xs">
              {(
                [
                  ["Servidores", c.config.servers],
                  ["Clientes", c.config.clients],
                  ["Duração virtual", `${c.config.durationMs / 1000} s`],
                  ["Latência", `${c.config.minLatencyMs}–${c.config.maxLatencyMs} ms`],
                  ["Perda", pct(c.config.dropRate)],
                  ["Duplicação", pct(c.config.dupRate)],
                  ["Crashes/s", c.config.crashRate],
                  ["Partições/s", c.config.partitionRate],
                ] as const
              ).map(([k, v]) => (
                <div key={k} className="flex justify-between border-b border-ink-800 pb-1.5">
                  <dt className="text-ink-400">{k}</dt>
                  <dd className="font-mono text-ink-100">{v}</dd>
                </div>
              ))}
            </dl>
          </Card>
        </div>
      </div>
    </div>
  );
}
