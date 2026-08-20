import { useState, type ReactNode } from "react";
import { Terminal, MonitorCog, Wrench } from "lucide-react";
import { StepContainer } from "../step-container";
import { AGENT_PLATFORMS } from "../../setup-data";
import type { PlatformSetupStatus } from "../../types";
import { HookSourceIcon } from "@/pages/hooks/HookSourceIcon";
import { AgentPlatformPickerItem } from "../agent-platform-picker-item";
import { PlatformInstrumentationSheet } from "../platform-instrumentation-sheet";
import { platformStatusBadge } from "../platform-status-badge";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/Tabs";
import { DeviceAgentSetup } from "@/pages/device-agent/device-agent-setup";

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
        <div className="bg-secondary flex h-12 w-12 items-center justify-center">
          <Terminal className="text-foreground h-6 w-6" />
        </div>
      }
      title="Instrument agents"
      description="Choose how your team's AI coding assistants get instrumented. Deploy the Speakeasy device agent to manage every platform centrally, or set up hooks per platform by hand."
      onContinue={onComplete}
      continueLabel="Continue"
      showBack
      onBack={onBack}
    >
      <Tabs defaultValue="device-agent" className="gap-8">
        <TabsList className="grid h-auto w-full grid-cols-1 items-stretch gap-4 divide-x-0 border-0 bg-transparent p-0 sm:grid-cols-2">
          <ChoiceTab
            value="device-agent"
            icon={<MonitorCog className="h-5 w-5" />}
            title="Device Agent"
            desc="Deploy one agent that enforces required plugins and MCP config across every coding assistant, centrally."
          />
          <ChoiceTab
            value="manual"
            icon={<Wrench className="h-5 w-5" />}
            title="Manual Setup"
            desc="Set up Speakeasy hooks by hand for each AI coding assistant your team uses."
          />
        </TabsList>

        <TabsContent value="device-agent">
          <DeviceAgentSetup />
        </TabsContent>

        <TabsContent value="manual">
          <div className="space-y-3">
            <div className="flex items-center justify-between">
              <span className="text-muted-foreground text-sm">
                {completedCount} of {availablePlatforms.length} platforms
                configured
              </span>
            </div>

            {availablePlatforms.map((platform) => {
              const status = platformStatus[platform.id] ?? "not_started";

              return (
                <AgentPlatformPickerItem
                  key={platform.id}
                  platformId={platform.id}
                  name={platform.name}
                  description={platform.description}
                  complete={status === "complete"}
                  statusBadge={platformStatusBadge(status)}
                  onClick={() => setDrawerPlatformId(platform.id)}
                />
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
                      className="border-border bg-card flex cursor-not-allowed items-center gap-3 border p-3 opacity-50"
                    >
                      <div className="bg-secondary flex h-8 w-8 flex-shrink-0 items-center justify-center">
                        <HookSourceIcon
                          source={platform.id}
                          className="h-4 w-4"
                        />
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
        </TabsContent>
      </Tabs>
    </StepContainer>
  );
}

// ChoiceTab is a full-width bordered card that doubles as a tab trigger, so the
// device-agent vs manual choice reads as a primary decision rather than a small
// text tab. The active card gets a primary border + ring.
function ChoiceTab({
  value,
  icon,
  title,
  desc,
}: {
  value: string;
  icon: ReactNode;
  title: string;
  desc: ReactNode;
}): JSX.Element {
  return (
    <TabsTrigger
      value={value}
      // Neutralize the segmented TabsTrigger base (mono/uppercase/tracked) for
      // the card body; the title span re-applies the mono eyebrow look itself.
      // The active card reads as the "front sheet": white fill on the gray
      // page, ink border + ring; inactive cards stay transparent and recede.
      className="border-border data-[state=active]:border-primary data-[state=active]:bg-card data-[state=active]:text-foreground data-[state=active]:ring-1 data-[state=active]:ring-primary h-auto flex-col items-start justify-start gap-2 border bg-transparent p-5 text-left font-sans text-sm tracking-normal normal-case whitespace-normal"
    >
      <div className="flex w-full items-center gap-2">
        <span className="text-foreground">{icon}</span>
        <span className="text-foreground text-base font-medium">{title}</span>
      </div>
      <span className="text-muted-foreground text-sm font-normal">{desc}</span>
    </TabsTrigger>
  );
}
