import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { MODE_LABELS } from "@/lib/mode-labels";
import { PROVIDER_INFO } from "@/lib/provider-info";
import type { Mode } from "@/lib/devidp";

export function CapabilitiesCard({ mode }: { mode: Mode }) {
  const info = PROVIDER_INFO[mode];
  return (
    <Card size="sm" className="!rounded-md">
      <CardHeader>
        <div className="text-[10px] uppercase tracking-wider text-muted-foreground font-mono">
          Provider
        </div>
        <CardTitle className="font-mono">{MODE_LABELS[mode]}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="flex flex-wrap gap-1.5">
          {info.capabilities.map((c) => (
            <CapabilityBadge key={c}>{c}</CapabilityBadge>
          ))}
        </div>
        <p className="text-sm text-muted-foreground leading-relaxed">
          {info.longDescription}
        </p>
      </CardContent>
    </Card>
  );
}

function CapabilityBadge({ children }: { children: React.ReactNode }) {
  return (
    <span className="inline-flex items-center px-2 py-0.5 rounded-sm text-[10px] font-mono uppercase tracking-wider bg-[var(--retro-yellow)]/20 text-foreground border border-[var(--retro-yellow)]/40">
      {children}
    </span>
  );
}
