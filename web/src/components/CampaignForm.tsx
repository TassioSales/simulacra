import { useMutation, useQuery } from "@tanstack/react-query";
import clsx from "clsx";
import { Bug, Play, Sparkles } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { useNavigate } from "react-router";
import { api, type RunConfig } from "../lib/api";
import { nf } from "../lib/format";
import { Badge, Button, Card, CardHeader, ErrorBox, Segmented, Spinner } from "./ui";

type Preset = { id: string; label: string; hint: string; apply: (c: RunConfig) => { config: RunConfig; seeds?: number } };

const presets: Preset[] = [
  {
    id: "calm",
    label: "Rede calma",
    hint: "sem crashes nem partições",
    apply: (c) => ({ config: { ...c, dropRate: 0, dupRate: 0, slowRate: 0, crashRate: 0, partitionRate: 0 } }),
  },
  {
    id: "default",
    label: "Padrão",
    hint: "ambiente moderadamente hostil",
    apply: (c) => ({ config: { ...c, dropRate: 0.02, dupRate: 0.01, slowRate: 0.01, crashRate: 0.6, partitionRate: 0.5 } }),
  },
  {
    id: "hostile",
    label: "Hostil",
    hint: "10% de perda, falhas frequentes",
    apply: (c) => ({ config: { ...c, dropRate: 0.1, dupRate: 0.05, slowRate: 0.05, crashRate: 1.5, partitionRate: 1.5 } }),
  },
  {
    id: "vote",
    label: "Caçar o voto perdido",
    hint: "o bug raro: 20 mil seeds",
    apply: (c) => ({
      config: { ...c, bugs: ["vote-not-persisted"], dropRate: 0.02, dupRate: 0.01, crashRate: 1.5, partitionRate: 1.5, servers: 3 },
      seeds: 20000,
    }),
  },
];

function Field({ label, value, children }: { label: string; value?: ReactNode; children: ReactNode }) {
  return (
    <label className="block">
      <div className="mb-1.5 flex items-baseline justify-between text-xs">
        <span className="text-ink-300">{label}</span>
        {value !== undefined && <span className="font-mono text-ink-100 tabular">{value}</span>}
      </div>
      {children}
    </label>
  );
}

function Range(props: { value: number; min: number; max: number; step: number; onChange: (v: number) => void; label: string }) {
  return (
    <input
      type="range"
      aria-label={props.label}
      className="w-full"
      value={props.value}
      min={props.min}
      max={props.max}
      step={props.step}
      onChange={(e) => props.onChange(Number(e.target.value))}
    />
  );
}

export function CampaignForm() {
  const navigate = useNavigate();
  const systems = useQuery({ queryKey: ["systems"], queryFn: api.systems });
  const defaults = useQuery({ queryKey: ["defaults"], queryFn: api.defaults });
  const [config, setConfig] = useState<RunConfig | null>(null);
  const [seeds, setSeeds] = useState(1000);
  const [startSeed, setStartSeed] = useState("");

  useEffect(() => {
    if (defaults.data && !config) setConfig(defaults.data);
  }, [defaults.data, config]);

  const create = useMutation({
    mutationFn: () => api.create({ config: config!, seeds, startSeed: Number(startSeed) || 0 }),
    onSuccess: (c) => navigate(`/campaigns/${c.id}`),
  });

  if (systems.isError) return <ErrorBox error={systems.error} />;
  if (!config || !systems.data) return <Spinner label="Carregando sistemas…" />;
  const system = systems.data.find((s) => s.name === config.system) ?? systems.data[0]!;
  const set = <K extends keyof RunConfig>(k: K, v: RunConfig[K]) => setConfig({ ...config, [k]: v });
  const toggleBug = (id: string) =>
    set("bugs", config.bugs.includes(id) ? config.bugs.filter((b) => b !== id) : [...config.bugs, id]);

  return (
    <Card>
      <CardHeader
        title="Nova campanha"
        hint="Cada seed é um universo: rede, relógio, crashes e partições derivados dela."
      />
      <form
        className="space-y-6 p-5"
        onSubmit={(e) => {
          e.preventDefault();
          create.mutate();
        }}
      >
        <div>
          <div className="mb-2 text-xs text-ink-300">Cenários prontos</div>
          <div className="grid grid-cols-2 gap-2">
            {presets.map((p) => (
              <button
                type="button"
                key={p.id}
                onClick={() => {
                  const r = p.apply(config);
                  setConfig(r.config);
                  if (r.seeds) setSeeds(r.seeds);
                }}
                className="rounded-lg border border-ink-700 bg-ink-900/60 px-3 py-2 text-left transition-colors hover:border-ink-500"
              >
                <div className="flex items-center gap-1.5 text-xs font-medium text-ink-100">
                  {p.id === "vote" && <Sparkles className="size-3 text-warn" />}
                  {p.label}
                </div>
                <div className="text-[11px] text-ink-400">{p.hint}</div>
              </button>
            ))}
          </div>
        </div>

        <div>
          <div className="mb-1 flex items-center gap-2 text-xs text-ink-300">
            Sistema sob teste <Badge tone="info">{system.title}</Badge>
          </div>
          <p className="text-xs leading-relaxed text-ink-400">{system.description}</p>
        </div>

        <fieldset>
          <legend className="mb-2 flex items-center gap-2 text-xs text-ink-300">
            <Bug className="size-3.5" /> Bugs injetados
            <span className="text-ink-500">nenhum = implementação correta</span>
          </legend>
          <div className="space-y-2">
            {system.bugs.map((b) => {
              const on = config.bugs.includes(b.id);
              return (
                <label
                  key={b.id}
                  className={clsx(
                    "flex cursor-pointer gap-3 rounded-lg border px-3 py-2.5 transition-colors",
                    on ? "border-fail/50 bg-fail/5" : "border-ink-700 bg-ink-900/40 hover:border-ink-500",
                  )}
                >
                  <input type="checkbox" className="mt-0.5 accent-[var(--color-fail)]" checked={on} onChange={() => toggleBug(b.id)} />
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="text-sm font-medium text-ink-100">{b.title}</span>
                      <code className="font-mono text-[10px] text-ink-400">{b.id}</code>
                      {b.hint && <Badge tone="warn">raro</Badge>}
                    </div>
                    <p className="mt-0.5 text-xs leading-relaxed text-ink-400">{b.description}</p>
                    <p className="mt-1 text-[11px] text-ink-500">detectado por: {b.detects}</p>
                    {b.hint && on && <p className="mt-1 text-[11px] text-warn">{b.hint}</p>}
                  </div>
                </label>
              );
            })}
          </div>
        </fieldset>

        <div className="grid grid-cols-2 gap-x-5 gap-y-4">
          <Field label="Servidores">
            <Segmented value={config.servers} onChange={(v) => set("servers", v)} options={[3, 5, 7].map((n) => ({ value: n, label: n }))} />
          </Field>
          <Field label="Clientes" value={config.clients}>
            <Range label="Clientes" value={config.clients} min={1} max={10} step={1} onChange={(v) => set("clients", v)} />
          </Field>
          <Field label="Duração virtual" value={`${(config.durationMs / 1000).toFixed(0)} s`}>
            <Range label="Duração" value={config.durationMs} min={2000} max={30000} step={1000} onChange={(v) => set("durationMs", v)} />
          </Field>
          <Field label="Latência máx." value={`${config.maxLatencyMs} ms`}>
            <Range label="Latência" value={config.maxLatencyMs} min={2} max={100} step={1} onChange={(v) => set("maxLatencyMs", v)} />
          </Field>
          <Field label="Perda de mensagens" value={`${(config.dropRate * 100).toFixed(0)}%`}>
            <Range label="Perda" value={config.dropRate} min={0} max={0.3} step={0.01} onChange={(v) => set("dropRate", v)} />
          </Field>
          <Field label="Duplicação" value={`${(config.dupRate * 100).toFixed(0)}%`}>
            <Range label="Duplicação" value={config.dupRate} min={0} max={0.3} step={0.01} onChange={(v) => set("dupRate", v)} />
          </Field>
          <Field label="Crashes / s" value={config.crashRate.toFixed(1)}>
            <Range label="Crashes" value={config.crashRate} min={0} max={4} step={0.1} onChange={(v) => set("crashRate", v)} />
          </Field>
          <Field label="Partições / s" value={config.partitionRate.toFixed(1)}>
            <Range label="Partições" value={config.partitionRate} min={0} max={4} step={0.1} onChange={(v) => set("partitionRate", v)} />
          </Field>
        </div>

        <div className="grid grid-cols-[1fr_auto] items-end gap-4">
          <Field label="Seeds" value={nf.format(seeds)}>
            <Segmented
              value={seeds}
              onChange={setSeeds}
              options={[200, 1000, 5000, 20000].map((n) => ({ value: n, label: nf.format(n) }))}
            />
          </Field>
          <Field label="Seed inicial">
            <input
              inputMode="numeric"
              placeholder="aleatória"
              value={startSeed}
              onChange={(e) => setStartSeed(e.target.value.replace(/\D/g, "").slice(0, 12))}
              className="h-8 w-28 rounded-lg border border-ink-700 bg-ink-900 px-2.5 font-mono text-xs text-ink-100 placeholder:text-ink-500 focus:border-phos focus:outline-none"
            />
          </Field>
        </div>

        {create.isError && <ErrorBox error={create.error} />}
        <Button type="submit" variant="primary" className="w-full" loading={create.isPending} icon={<Play className="size-4" />}>
          Rodar {nf.format(seeds)} seeds
        </Button>
      </form>
    </Card>
  );
}
