import { useState } from "react";
import { Terminal, ChevronRight } from "lucide-react";
import { StepContainer } from "../step-container";
import { AGENT_PLATFORMS } from "../../setup-data";
import type { PlatformSetupStatus } from "../../types";
import { HookSourceIcon } from "@/pages/hooks/HookSourceIcon";
import { cn } from "@/lib/utils";
import { PlatformInstrumentationSheet } from "../platform-instrumentation-sheet";
import { PLATFORM_LOGOS, INVERT_LOGO_IN_DARK } from "../platform-logos";
import { platformStatusBadge } from "../platform-status-badge";

interface InstrumentAgentsStepProps {
  onComplete: () => void;
  onBack: () => void;
}

export function InstrumentAgentsStep({
  onComplete,
  onBack,
}: InstrumentAgentsStepProps): JSX.Element {
  const [drawerPlatformId, setDrawerPlatformId] = useState<string | null>(null);
  const [platformStatus, setPlatformStatus] = useState<
    Record<string, PlatformSetupStatus>
  >(() =>
    Object.fromEntries(AGENT_PLATFORMS.map((p) => [p.id, "not_started"])),
  );

  const availablePlatforms = AGENT_PLATFORMS.filter(
    (p) => p.available !== false,
  );
  const comingSoonPlatforms = AGENT_PLATFORMS.filter(
    (p) => p.available === false,
  );
  const completedCount = availablePlatforms.filter(
    (p) => platformStatus[p.id] === "complete",
  ).length;

  return (
    <StepContainer
      icon={
        <div className="bg-secondary flex h-12 w-12 items-center justify-center rounded-lg">
          <Terminal className="text-foreground h-6 w-6" />
        </div>
      }
      title="Instrument agent platforms"
      description="Set up Speakeasy hooks for each AI coding assistant your team uses. Each platform has its own configuration steps."
      onContinue={onComplete}
      continueLabel="Continue"
      showBack
      onBack={onBack}
    >
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <span className="text-muted-foreground text-sm">
            {completedCount} of {availablePlatforms.length} platforms configured
          </span>
        </div>

        {availablePlatforms.map((platform) => {
          const status = platformStatus[platform.id] ?? "not_started";

          return (
            <button
              key={platform.id}
              type="button"
              onClick={() => setDrawerPlatformId(platform.id)}
              className={cn(
                "flex w-full items-center gap-4 rounded-lg border p-4 text-left transition-all",
                status === "complete"
                  ? "border-foreground/10 bg-secondary/20"
                  : "border-border bg-card hover:border-foreground/20",
              )}
            >
              <div
                className={cn(
                  "flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-lg",
                  status === "complete" ? "bg-foreground/10" : "bg-secondary",
                )}
              >
                {PLATFORM_LOGOS[platform.id] ? (
                  <img
                    src={PLATFORM_LOGOS[platform.id]}
                    alt={platform.name}
                    className={cn(
                      "h-5 w-5",
                      INVERT_LOGO_IN_DARK.has(platform.id) && "dark:invert",
                    )}
                  />
                ) : (
                  <span className="text-foreground text-sm font-semibold">
                    {platform.name.charAt(0)}
                  </span>
                )}
              </div>
              <div className="min-w-0 flex-1 space-y-1">
                <div className="flex items-center gap-2">
                  <p className="text-foreground text-sm font-medium">
                    {platform.name}
                  </p>
                  {platformStatusBadge(status)}
                </div>
                <p className="text-muted-foreground text-xs">
                  {platform.description}
                </p>
              </div>
              <ChevronRight className="text-muted-foreground h-4 w-4 flex-shrink-0" />
            </button>
          );
        })}

        {comingSoonPlatforms.length > 0 && (
          <div className="pt-3">
            <p className="text-muted-foreground mb-2 text-[11px] font-medium tracking-wider uppercase">
              Coming soon
            </p>
            <div className="grid grid-cols-2 gap-2">
              {comingSoonPlatforms.map((platform) => (
                <div
                  key={platform.id}
                  aria-disabled
                  className="border-border bg-card flex cursor-not-allowed items-center gap-3 rounded-lg border p-3 opacity-50"
                >
                  <div className="bg-secondary flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-md">
                    <HookSourceIcon source={platform.id} className="h-4 w-4" />
                  </div>
                  <div className="min-w-0 flex-1">
                    <p className="text-foreground truncate text-sm font-medium">
                      {platform.name}
                    </p>
                    <p className="text-muted-foreground truncate text-xs">
                      {platform.description}
                    </p>
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}
      </div>

      <PlatformInstrumentationSheet
        open={!!drawerPlatformId}
        onOpenChange={(open) => {
          if (!open) setDrawerPlatformId(null);
        }}
        initialPlatformId={drawerPlatformId ?? undefined}
        onPlatformStatusChange={(id, status) =>
          setPlatformStatus((prev) => ({ ...prev, [id]: status }))
        }
      />
    </StepContainer>
  );
}
