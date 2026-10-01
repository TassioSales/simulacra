import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter, Link, Route, Routes } from "react-router";
import "./index.css";
import { Layout } from "./components/Layout";
import { CampaignPage } from "./pages/CampaignPage";
import { HomePage } from "./pages/HomePage";
import { RunPage } from "./pages/RunPage";

const queryClient = new QueryClient({
  defaultOptions: { queries: { refetchOnWindowFocus: false, retry: 1 } },
});

function NotFound() {
  return (
    <div className="py-24 text-center">
      <p className="font-mono text-5xl text-ink-600">404</p>
      <p className="mt-3 text-ink-300">Página não encontrada.</p>
      <Link to="/" className="mt-6 inline-block text-sm text-phos hover:underline">
        Voltar ao início
      </Link>
    </div>
  );
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Routes>
          <Route element={<Layout />}>
            <Route index element={<HomePage />} />
            <Route path="campaigns/:id" element={<CampaignPage />} />
            <Route path="campaigns/:id/seeds/:seed" element={<RunPage />} />
            <Route path="*" element={<NotFound />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </QueryClientProvider>
  </StrictMode>,
);
