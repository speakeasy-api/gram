import { useState } from "react";
import { Terminal, Check, Copy, ChevronRight } from "lucide-react";
import { StepContainer } from "../step-container";
import { AGENT_PLATFORMS } from "../../mock-data";
import type { AgentPlatform, PlatformSetupStatus } from "../../types";
import { Badge } from "@speakeasy-api/moonshine";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

interface InstrumentAgentsStepProps {
  onComplete: () => void;
  onBack: () => void;
}

const PLATFORM_LOGOS: Record<string, string> = {
  claude: "/icons/platforms/claude.svg",
  codex: "/icons/platforms/openai.svg",
  cursor: "/icons/platforms/cursor.svg",
};

export function InstrumentAgentsStep({
  onComplete,
  onBack,
}: InstrumentAgentsStepProps) {
  const [expandedPlatform, setExpandedPlatform] = useState<string | null>(null);
  const [platformStatus, setPlatformStatus] = useState<
    Record<string, PlatformSetupStatus>
  >(() =>
    Object.fromEntries(AGENT_PLATFORMS.map((p) => [p.id, "not_started"])),
  );
  const [activeStepIndex, setActiveStepIndex] = useState<
    Record<string, number>
  >(() => Object.fromEntries(AGENT_PLATFORMS.map((p) => [p.id, 0])));
  const [copiedField, setCopiedField] = useState<string | null>(null);

  const completedCount = Object.values(platformStatus).filter(
    (s) => s === "complete",
  ).length;

  const toggleExpand = (platformId: string) => {
    if (expandedPlatform === platformId) {
      setExpandedPlatform(null);
    } else {
      setExpandedPlatform(platformId);
      if (platformStatus[platformId] === "not_started") {
        setPlatformStatus((prev) => ({
          ...prev,
          [platformId]: "in_progress",
        }));
      }
    }
  };

  const advanceStep = (platformId: string, platform: AgentPlatform) => {
    const currentIdx = activeStepIndex[platformId] ?? 0;
    if (currentIdx < platform.setupSteps.length - 1) {
      setActiveStepIndex((prev) => ({
        ...prev,
        [platformId]: currentIdx + 1,
      }));
    } else {
      setPlatformStatus((prev) => ({ ...prev, [platformId]: "complete" }));
      setExpandedPlatform(null);
    }
  };

  const goBackStep = (platformId: string) => {
    const currentIdx = activeStepIndex[platformId] ?? 0;
    if (currentIdx > 0) {
      setActiveStepIndex((prev) => ({
        ...prev,
        [platformId]: currentIdx - 1,
      }));
    }
  };

  const copyToClipboard = async (text: string, field: string) => {
    await navigator.clipboard.writeText(text);
    setCopiedField(field);
    setTimeout(() => setCopiedField(null), 2000);
  };

  const statusBadge = (status: PlatformSetupStatus) => {
    switch (status) {
      case "complete":
        return (
          <Badge variant="success" background>
            <Badge.Text>Complete</Badge.Text>
          </Badge>
        );
      case "in_progress":
        return (
          <Badge variant="neutral" background>
            <Badge.Text>In progress</Badge.Text>
          </Badge>
        );
      default:
        return null;
    }
  };

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
        {/* Summary */}
        <div className="flex items-center justify-between">
          <span className="text-muted-foreground text-sm">
            {completedCount} of {AGENT_PLATFORMS.length} platforms configured
          </span>
        </div>

        {/* Platform cards */}
        {AGENT_PLATFORMS.map((platform) => {
          const status = platformStatus[platform.id] ?? "not_started";
          const isExpanded = expandedPlatform === platform.id;
          const currentStepIdx = activeStepIndex[platform.id] ?? 0;
          const currentStep = platform.setupSteps[currentStepIdx];

          return (
            <div
              key={platform.id}
              className={cn(
                "rounded-lg border transition-all",
                isExpanded
                  ? "border-foreground/20 bg-secondary/30"
                  : status === "complete"
                    ? "border-foreground/10 bg-secondary/20"
                    : "border-border bg-card hover:border-foreground/20",
              )}
            >
              {/* Card header — always visible */}
              <button
                type="button"
                onClick={() => toggleExpand(platform.id)}
                className="flex w-full items-center gap-4 p-4 text-left"
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
                      className="h-5 w-5"
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
                    {statusBadge(status)}
                  </div>
                  <p className="text-muted-foreground text-xs">
                    {platform.description}
                  </p>
                </div>
                <ChevronRight
                  className={cn(
                    "text-muted-foreground h-4 w-4 flex-shrink-0 transition-transform",
                    isExpanded && "rotate-90",
                  )}
                />
              </button>

              {/* Expanded setup wizard */}
              {isExpanded && currentStep && (
                <div className="px-4 pb-4">
                  {/* Progress dots */}
                  <div className="flex items-center gap-1.5 pb-4">
                    {platform.setupSteps.map((_, idx) => (
                      <button
                        key={idx}
                        type="button"
                        onClick={() =>
                          idx <= currentStepIdx &&
                          setActiveStepIndex((prev) => ({
                            ...prev,
                            [platform.id]: idx,
                          }))
                        }
                        className={cn(
                          "h-1 rounded-full transition-all",
                          idx === currentStepIdx
                            ? "bg-foreground w-6"
                            : idx < currentStepIdx
                              ? "bg-foreground/40 hover:bg-foreground/60 w-4 cursor-pointer"
                              : "bg-border w-4",
                        )}
                      />
                    ))}
                    <span className="text-muted-foreground ml-auto text-[11px] tabular-nums">
                      {currentStepIdx + 1}/{platform.setupSteps.length}
                    </span>
                  </div>

                  {/* Step content */}
                  <div className="bg-background border-border rounded-lg border p-4">
                    <p className="text-muted-foreground text-[11px] font-medium tracking-wider uppercase">
                      Step {currentStepIdx + 1}
                    </p>
                    <h4 className="text-foreground mt-1 text-sm font-medium">
                      {currentStep.title}
                    </h4>
                    <p className="text-muted-foreground mt-1.5 text-xs leading-relaxed">
                      {currentStep.description}
                    </p>

                    {currentStep.code && (
                      <div className="mt-3 overflow-hidden rounded-md bg-zinc-950">
                        <div className="flex items-center justify-between px-3 py-1.5">
                          <span className="text-[10px] tracking-wider text-zinc-500 uppercase">
                            {currentStep.language ?? "shell"}
                          </span>
                          <Button
                            variant="ghost"
                            size="sm"
                            className="h-6 px-2 text-[11px] text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200"
                            onClick={() =>
                              copyToClipboard(
                                currentStep.code!,
                                `${platform.id}-${currentStepIdx}`,
                              )
                            }
                          >
                            {copiedField ===
                            `${platform.id}-${currentStepIdx}` ? (
                              <>
                                <Check className="mr-1 h-3 w-3" />
                                Copied
                              </>
                            ) : (
                              <>
                                <Copy className="mr-1 h-3 w-3" />
                                Copy
                              </>
                            )}
                          </Button>
                        </div>
                        <pre className="overflow-x-auto px-3 pb-3 text-[13px] leading-relaxed">
                          <code className="text-zinc-200">
                            {currentStep.code}
                          </code>
                        </pre>
                      </div>
                    )}

                    {/* Navigation */}
                    <div className="mt-4 flex items-center justify-between">
                      <Button
                        variant="ghost"
                        size="sm"
                        className="text-muted-foreground h-8 text-xs"
                        disabled={currentStepIdx === 0}
                        onClick={() => goBackStep(platform.id)}
                      >
                        Back
                      </Button>
                      <Button
                        size="sm"
                        className="bg-accent hover:bg-accent/90 text-accent-foreground h-8 text-xs"
                        onClick={() => advanceStep(platform.id, platform)}
                      >
                        {currentStepIdx === platform.setupSteps.length - 1
                          ? "Mark complete"
                          : "Next step"}
                      </Button>
                    </div>
                  </div>
                </div>
              )}
            </div>
          );
        })}
      </div>
    </StepContainer>
  );
}
