import { CircleCheck, ExternalLink, Settings2 } from "lucide-react";
import { Link } from "@tanstack/react-router";
import { cn } from "@/lib/utils";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { useGramMode } from "@/hooks/use-gram-mode";
import { MODES, type Mode } from "@/lib/devidp";
import { MODE_LABELS, MODE_SUBTITLES } from "@/lib/mode-labels";
import { PROVIDER_INFO } from "@/lib/provider-info";
import { EnvReadout } from "@/components/EnvReadout";

/**
 * Right-column sidebar — answers two questions:
 *  1. Which provider is Gram talking to right now?
 *  2. What does that provider actually do?
 *
 * Switching providers is intentionally pushed to the deeper /providers/$mode
 * route since it requires editing env vars and a Gram restart — not a
 * one-click action.
 */
export function ActiveProviderPanel() {
  const { data, isLoading } = useGramMode();
  const activeMode = data?.mode ?? null;

  return (
    <div className="space-y-4 lg:sticky lg:top-4">
      <Card>
        <CardHeader className="border-b pb-4">
          <div className="text-[10px] uppercase tracking-wider text-muted-foreground font-mono">
            Active provider
          </div>
          <CardTitle className="font-mono text-lg flex items-center gap-2">
            {isLoading ? (
              "…"
            ) : activeMode ? (
              <>
                <span
                  className="inline-block size-2 rounded-full bg-[var(--retro-green)] shadow-[0_0_6px_var(--retro-green)]"
                  aria-hidden="true"
                />
                {MODE_LABELS[activeMode]}
              </>
            ) : (
              "none"
            )}
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          {isLoading ? (
            <div className="text-xs text-muted-foreground">Loading…</div>
          ) : activeMode ? (
            <ActiveDetails mode={activeMode} />
          ) : (
            <UnconfiguredDetails />
          )}
        </CardContent>
      </Card>

      {activeMode && <SwitchProviderCard active={activeMode} />}

      <EnvReadout />
    </div>
  );
}

function ActiveDetails({ mode }: { mode: Mode }) {
  const info = PROVIDER_INFO[mode];
  return (
    <>
      <div className="flex items-start gap-2">
        <CircleCheck
          className="size-4 shrink-0 mt-0.5 text-[var(--retro-green)]"
          strokeWidth={2.5}
        />
        <div className="text-xs leading-relaxed">
          <span className="font-semibold">
            Gram is logging in via this provider.
          </span>
          <div className="text-muted-foreground mt-0.5">
            {MODE_SUBTITLES[mode]}
          </div>
        </div>
      </div>

      <div className="flex flex-wrap gap-1.5">
        {info.capabilities.map((c) => (
          <CapabilityBadge key={c}>{c}</CapabilityBadge>
        ))}
      </div>

      <p className="text-xs text-muted-foreground leading-relaxed">
        {info.longDescription}
      </p>

      <Button
        variant="outline"
        size="xs"
        className="w-full"
        render={
          <Link to="/providers/$mode" params={{ mode }}>
            <Settings2 />
            Provider details &amp; activation
          </Link>
        }
      />
    </>
  );
}

function UnconfiguredDetails() {
  return (
    <div className="text-xs text-muted-foreground leading-relaxed">
      <div className="font-medium text-foreground mb-1">
        No dev-idp mode detected.
      </div>
      Neither <code className="font-mono">SPEAKEASY_API_URL</code> nor{" "}
      <code className="font-mono">WORKOS_API_URL</code> points back at the
      dev-idp — Gram is talking to an external upstream.
    </div>
  );
}

function SwitchProviderCard({ active }: { active: Mode }) {
  const others = MODES.filter((m) => m !== active);
  return (
    <Card size="sm">
      <CardHeader>
        <CardTitle className="text-xs font-mono uppercase tracking-wider text-muted-foreground">
          Switch provider
        </CardTitle>
      </CardHeader>
      <CardContent>
        <ul className="-my-1">
          {others.map((m) => (
            <li key={m}>
              <Link
                to="/providers/$mode"
                params={{ mode: m }}
                className={cn(
                  "flex items-center gap-2 py-1.5 -mx-2 px-2 rounded",
                  "text-sm hover:bg-muted/60 group/link transition-colors",
                )}
              >
                <span className="font-mono font-medium flex-1 truncate">
                  {MODE_LABELS[m]}
                </span>
                <ExternalLink className="size-3 text-muted-foreground opacity-0 group-hover/link:opacity-100 transition-opacity" />
              </Link>
            </li>
          ))}
        </ul>
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
