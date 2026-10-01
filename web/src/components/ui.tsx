import clsx from "clsx";
import { LoaderCircle } from "lucide-react";
import type { ButtonHTMLAttributes, ReactNode } from "react";

export function Card({ className, children }: { className?: string; children: ReactNode }) {
  return (
    <section className={clsx("rounded-xl border border-ink-700 bg-ink-850/80 backdrop-blur-sm", className)}>
      {children}
    </section>
  );
}

export function CardHeader({ title, hint, right }: { title: ReactNode; hint?: ReactNode; right?: ReactNode }) {
  return (
    <header className="flex items-start justify-between gap-4 border-b border-ink-700 px-5 py-3.5">
      <div className="min-w-0">
        <h2 className="text-sm font-semibold tracking-tight text-ink-100">{title}</h2>
        {hint && <p className="mt-0.5 text-xs text-ink-400">{hint}</p>}
      </div>
      {right}
    </header>
  );
}

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: "primary" | "ghost" | "danger" | "outline";
  size?: "sm" | "md";
  loading?: boolean;
  icon?: ReactNode;
};

export function Button({ variant = "outline", size = "md", loading, icon, className, children, disabled, ...rest }: ButtonProps) {
  return (
    <button
      {...rest}
      disabled={disabled || loading}
      className={clsx(
        "inline-flex items-center justify-center gap-2 rounded-lg font-medium transition-colors",
        "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-phos disabled:cursor-not-allowed disabled:opacity-50",
        size === "sm" ? "h-8 px-3 text-xs" : "h-10 px-4 text-sm",
        variant === "primary" && "bg-phos text-ink-950 hover:bg-[#9af7b3]",
        variant === "outline" && "border border-ink-600 bg-ink-800 text-ink-100 hover:border-ink-500 hover:bg-ink-750",
        variant === "ghost" && "text-ink-300 hover:bg-ink-800 hover:text-ink-100",
        variant === "danger" && "border border-fail-dim bg-fail/10 text-fail hover:bg-fail/20",
        className,
      )}
    >
      {loading ? <LoaderCircle className="size-4 animate-spin" /> : icon}
      {children}
    </button>
  );
}

export function Badge({ tone = "neutral", children, className }: { tone?: "neutral" | "pass" | "fail" | "warn" | "info"; children: ReactNode; className?: string }) {
  return (
    <span
      className={clsx(
        "inline-flex items-center gap-1.5 rounded-md px-2 py-0.5 text-[11px] font-medium whitespace-nowrap",
        tone === "neutral" && "bg-ink-700 text-ink-200",
        tone === "pass" && "bg-phos/12 text-phos",
        tone === "fail" && "bg-fail/12 text-fail",
        tone === "warn" && "bg-warn/12 text-warn",
        tone === "info" && "bg-sky/12 text-sky",
        className,
      )}
    >
      {children}
    </span>
  );
}

export function Stat({ label, value, sub, tone }: { label: string; value: ReactNode; sub?: ReactNode; tone?: "pass" | "fail" | "warn" }) {
  return (
    <div className="rounded-xl border border-ink-700 bg-ink-850/80 px-4 py-3">
      <div className="text-[11px] font-medium tracking-wide text-ink-400 uppercase">{label}</div>
      <div
        className={clsx(
          "mt-1 font-mono text-2xl font-semibold tabular",
          tone === "pass" && "text-phos",
          tone === "fail" && "text-fail",
          tone === "warn" && "text-warn",
        )}
      >
        {value}
      </div>
      {sub && <div className="mt-0.5 text-xs text-ink-400">{sub}</div>}
    </div>
  );
}

export function Spinner({ label }: { label?: string }) {
  return (
    <div className="flex items-center justify-center gap-3 py-16 text-sm text-ink-400">
      <LoaderCircle className="size-5 animate-spin text-phos" />
      {label}
    </div>
  );
}

export function ErrorBox({ error }: { error: unknown }) {
  return (
    <div className="rounded-xl border border-fail-dim bg-fail/5 px-5 py-4 text-sm text-fail">
      {error instanceof Error ? error.message : String(error)}
    </div>
  );
}

export function Segmented<T extends string | number>({
  value,
  options,
  onChange,
}: {
  value: T;
  options: { value: T; label: ReactNode }[];
  onChange: (v: T) => void;
}) {
  return (
    <div className="inline-flex rounded-lg border border-ink-700 bg-ink-900 p-0.5">
      {options.map((o) => (
        <button
          key={String(o.value)}
          type="button"
          onClick={() => onChange(o.value)}
          className={clsx(
            "rounded-md px-3 py-1 text-xs font-medium transition-colors",
            o.value === value ? "bg-ink-700 text-ink-100" : "text-ink-400 hover:text-ink-200",
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

export function Kbd({ children }: { children: ReactNode }) {
  return <kbd className="rounded border border-ink-600 bg-ink-800 px-1.5 py-0.5 font-mono text-[10px] text-ink-300">{children}</kbd>;
}
