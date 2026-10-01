import { Link, Outlet } from "react-router";

export function Logo() {
  return (
    <svg viewBox="0 0 32 32" className="size-7" aria-hidden>
      <rect width="32" height="32" rx="7" className="fill-ink-800" />
      <path
        d="M5 22 L11 10 L16 18 L21 8 L27 22"
        fill="none"
        stroke="var(--color-phos)"
        strokeWidth="2.6"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <circle cx="21" cy="8" r="2.4" fill="var(--color-fail)" />
    </svg>
  );
}

export function Layout() {
  return (
    <div className="flex min-h-screen flex-col">
      <header className="sticky top-0 z-30 border-b border-ink-700/80 bg-ink-900/85 backdrop-blur-md">
        <div className="mx-auto flex h-14 max-w-[1500px] items-center gap-6 px-4 sm:px-6">
          <Link to="/" className="flex items-center gap-2.5">
            <Logo />
            <span className="font-mono text-[15px] font-semibold tracking-tight">simulacra</span>
          </Link>
          <span className="hidden text-xs text-ink-400 md:block">simulação determinística para sistemas distribuídos</span>
          <nav className="ml-auto flex items-center gap-1 text-sm">
            <Link to="/" className="rounded-md px-3 py-1.5 text-ink-300 hover:bg-ink-800 hover:text-ink-100">
              Campanhas
            </Link>
          </nav>
        </div>
      </header>
      <main className="mx-auto w-full max-w-[1500px] flex-1 px-4 py-6 sm:px-6">
        <Outlet />
      </main>
      <footer className="border-t border-ink-800 py-5 text-center text-xs text-ink-500">
        Mesma seed, mesma execução. Cada falha encontrada é um replay exato.
      </footer>
    </div>
  );
}
