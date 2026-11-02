import { Lock } from "lucide-react";

export function RiskContentNotice(): JSX.Element {
  return (
    <span className="text-muted-foreground inline-flex items-center gap-1.5 text-xs">
      <Lock className="size-3 shrink-0" aria-hidden="true" />
      <span>Hidden during impersonation</span>
    </span>
  );
}
