import { useQuery } from "@tanstack/react-query";
import { ArrowRight, FlaskConical } from "lucide-react";
import { Link } from "react-router";
import { CampaignForm } from "../components/CampaignForm";
import { StatusBadge } from "../components/StatusBadge";
import { Card, CardHeader, ErrorBox, Spinner } from "../components/ui";
import { api, violationLabel } from "../lib/api";
import { fmtDuration, nf, relTime } from "../lib/format";

function Hero() {
  const steps = [
    ["1", "Semente", "Uma seed gera a rede, o relógio, a ordem dos eventos e o plano de falhas."],
    ["2", "Simulação", "O sistema roda milhares de vezes por segundo, num único thread, sob crashes e partições."],
    ["3", "Verificação", "Linearizabilidade e invariantes do Raft são checados em cada execução."],
    ["4", "Replay", "Uma falha vira uma seed: reproduzível bit a bit, minimizável, inspecionável."],
  ];
  return (
    <div className="mb-6 grid gap-3 md:grid-cols-4">
      {steps.map(([n, title, text]) => (
        <div key={n} className="rounded-xl border border-ink-700/70 bg-ink-850/50 px-4 py-3">
          <div className="flex items-center gap-2">
            <span className="grid size-5 place-items-center rounded-full bg-phos/15 font-mono text-[10px] text-phos">{n}</span>
            <span className="text-sm font-medium">{title}</span>
          </div>
          <p className="mt-1.5 text-xs leading-relaxed text-ink-400">{text}</p>
        </div>
      ))}
    </div>
  );
}

function CampaignList() {
  const q = useQuery({
    queryKey: ["campaigns"],
    queryFn: api.campaigns,
    refetchInterval: (query) => (query.state.data?.some((c) => c.status === "running") ? 1000 : false),
  });
  if (q.isPending) return <Spinner label="Carregando campanhas…" />;
  if (q.isError) return <ErrorBox error={q.error} />;
  if (q.data.length === 0) {
    return (
      <div className="flex flex-col items-center gap-3 px-6 py-20 text-center">
        <FlaskConical className="size-10 text-ink-600" />
        <p className="text-sm text-ink-300">Nenhuma campanha ainda.</p>
        <p className="max-w-sm text-xs text-ink-500">
          Comece pelo Raft correto para ver que ele sobrevive às falhas, depois ligue um bug e veja o simulador encontrá-lo.
        </p>
      </div>
    );
  }
  return (
    <ul className="divide-y divide-ink-700/70">
      {q.data.map((c) => {
        const rate = c.elapsedMs > 0 ? (c.done / c.elapsedMs) * 1000 : 0;
        return (
          <li key={c.id}>
            <Link to={`/campaigns/${c.id}`} className="group flex items-center gap-4 px-5 py-3.5 transition-colors hover:bg-ink-800/60">
              <div className="w-24 shrink-0">
                <StatusBadge status={c.status} failed={c.failed} />
              </div>
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm">
                  <span className="font-medium">{c.config.system}</span>
                  {c.config.bugs.length === 0 ? (
                    <span className="text-xs text-ink-400">implementação correta</span>
                  ) : (
                    c.config.bugs.map((b) => (
                      <code key={b} className="rounded bg-fail/10 px-1.5 py-0.5 font-mono text-[10px] text-fail">
                        {b}
                      </code>
                    ))
                  )}
                </div>
                <div className="mt-1 flex flex-wrap gap-x-3 text-xs text-ink-400">
                  <span className="tabular">
                    {nf.format(c.done)}/{nf.format(c.seeds)} seeds
                  </span>
                  <span>{c.config.servers} servidores</span>
                  <span className="tabular">{nf.format(Math.round(rate))} seeds/s</span>
                  <span>{fmtDuration(c.elapsedMs)}</span>
                  {Object.keys(c.kinds).map((k) => (
                    <span key={k} className="text-fail">
                      {violationLabel[k] ?? k}
                    </span>
                  ))}
                </div>
              </div>
              <div className="hidden text-right sm:block">
                <div className={c.failed ? "font-mono text-lg text-fail tabular" : "font-mono text-lg text-phos tabular"}>
                  {nf.format(c.failed)}
                </div>
                <div className="text-[11px] text-ink-500">{relTime(c.createdAt)}</div>
              </div>
              <ArrowRight className="size-4 text-ink-600 transition-transform group-hover:translate-x-0.5 group-hover:text-ink-300" />
            </Link>
          </li>
        );
      })}
    </ul>
  );
}

export function HomePage() {
  return (
    <>
      <Hero />
      <div className="grid items-start gap-6 lg:grid-cols-[440px_1fr]">
        <div className="lg:sticky lg:top-20">
          <CampaignForm />
        </div>
        <Card>
          <CardHeader title="Campanhas" hint="Falhas por campanha à direita. Clique para ver as seeds." />
          <CampaignList />
        </Card>
      </div>
    </>
  );
}
