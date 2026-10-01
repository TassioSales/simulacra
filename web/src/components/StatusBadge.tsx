import type { Summary } from "../lib/api";
import { Badge } from "./ui";

export function StatusBadge({ status, failed }: { status: Summary["status"]; failed: number }) {
  if (status === "running") {
    return (
      <Badge tone="info">
        <span className="live-dot size-1.5 rounded-full bg-sky" /> rodando
      </Badge>
    );
  }
  if (status === "cancelled") return <Badge tone="warn">cancelada</Badge>;
  if (status === "interrupted") return <Badge tone="warn">interrompida</Badge>;
  return failed > 0 ? <Badge tone="fail">falhou</Badge> : <Badge tone="pass">passou</Badge>;
}
