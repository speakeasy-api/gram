import {
  dismissProjectGuide,
  markProjectGuideStarted,
} from "@/components/project-guide/projectGuideStores";
import { firstIncompleteStepIndex } from "@/components/project-guide/journeyStatus";
import {
  PROJECT_GUIDE_JOURNEYS,
  JOURNEY_STATUS_LABELS,
  type JourneyId,
  type JourneyMeta,
  type JourneyStatus,
} from "@/components/project-guide/journeys";
import { useProjectGuideProgress } from "@/components/project-guide/useProjectGuideProgress";
import { Button } from "@/components/ui/Button";
import { useSlugs } from "@/contexts/Sdk";
import { cn } from "@/lib/utils";
import { useState } from "react";

/**
 * The zero-data guide that takes project home's space when the gate opens it.
 * Two journeys, each ending in something the user watches happen. The cards are
 * an accordion: no overlay, no drawer, no navigation away.
 *
 * The step bodies are placeholders on this branch — the journeys land next.
 */
export function ProjectGuide(): JSX.Element {
  const { projectSlug } = useSlugs();
  const { statusByJourney, isPending: progressPending } =
    useProjectGuideProgress();
  const [expanded, setExpanded] = useState<JourneyId | null>(null);

  const toggle = (id: JourneyId) => {
    setExpanded((current) => (current === id ? null : id));
    // Expanding a card is the first real interaction, and what tells the gate a
    // run is under way. Writing this on render instead would flag every empty
    // project on first load.
    if (projectSlug) markProjectGuideStarted(projectSlug);
  };

  return (
    <div className="w-full pt-2 pb-6">
      <div className="flex items-start justify-between gap-4 pb-6">
        <div className="flex flex-col gap-2">
          <span className="text-eyebrow">Get started</span>
          <h2 className="text-foreground font-display text-[32px] leading-[1.05] font-thin tracking-[-0.03em]">
            Nothing here yet — two ways to start
          </h2>
          <p className="text-muted-foreground max-w-[62ch] text-[13px] leading-[1.6]">
            This project has no MCP servers, no policies, and no traffic. Either
            path below ends in something you can watch happen, in about five
            minutes.
          </p>
        </div>
        <Button
          variant="tertiary"
          size="sm"
          onClick={() => {
            if (projectSlug) dismissProjectGuide(projectSlug);
          }}
        >
          Dismiss
        </Button>
      </div>

      <div className="flex flex-col gap-4">
        {PROJECT_GUIDE_JOURNEYS.map((journey) => (
          <JourneyCard
            key={journey.id}
            journey={journey}
            status={statusByJourney[journey.id]}
            statusPending={progressPending}
            expanded={expanded === journey.id}
            onToggle={() => toggle(journey.id)}
          />
        ))}
      </div>
    </div>
  );
}

function JourneyCard({
  journey,
  status,
  statusPending,
  expanded,
  onToggle,
}: {
  journey: JourneyMeta;
  status: JourneyStatus;
  statusPending: boolean;
  expanded: boolean;
  onToggle: () => void;
}): JSX.Element {
  const currentStep = firstIncompleteStepIndex(status, journey.steps.length);
  const triggerId = `project-guide-${journey.id}-trigger`;
  const panelId = `project-guide-${journey.id}-panel`;

  return (
    <section className="bg-card border-border hover:border-foreground/40 border transition-colors">
      <h3>
        <button
          id={triggerId}
          type="button"
          onClick={onToggle}
          aria-controls={panelId}
          aria-expanded={expanded}
          className="flex w-full flex-col items-start gap-2 px-6.5 py-5 text-left"
        >
          <span className="flex w-full items-center gap-2.5">
            <span className="text-muted-foreground font-mono text-xs tracking-wider">
              {journey.index}
            </span>
            <span className="text-foreground text-[19px] leading-[1.25]">
              {journey.title}
            </span>
            <span
              role={statusPending ? "status" : undefined}
              aria-label={
                statusPending
                  ? `Loading ${journey.title} journey status`
                  : undefined
              }
              className="border-border text-muted-foreground ml-auto border px-1.5 py-px font-mono text-[9px] tracking-[0.08em] uppercase"
            >
              {statusPending ? "Loading" : JOURNEY_STATUS_LABELS[status]}
            </span>
          </span>
          <span className="text-muted-foreground max-w-[64ch] text-[13px] leading-[1.6]">
            {journey.win}
          </span>
        </button>
      </h3>

      {expanded && (
        <div
          id={panelId}
          role="region"
          aria-labelledby={triggerId}
          data-testid="journey-body"
          className="border-border border-t px-6.5 py-5"
        >
          <ol className="flex flex-wrap items-center gap-4 pb-4">
            {journey.steps.map((step, index) => (
              <li key={step} className="flex items-center gap-2">
                <span
                  aria-hidden="true"
                  className={cn(
                    "size-1.5 rounded-full",
                    index === currentStep ? "bg-foreground" : "bg-border",
                  )}
                />
                <span
                  aria-current={index === currentStep ? "step" : undefined}
                  className={cn(
                    "font-mono text-[11px] tracking-[0.04em]",
                    index === currentStep
                      ? "text-foreground"
                      : "text-muted-foreground",
                  )}
                >
                  {step}
                </span>
              </li>
            ))}
          </ol>
          <p className="text-muted-foreground text-[13px] leading-[1.6]">
            Step actions arrive with the journey itself.
          </p>
        </div>
      )}
    </section>
  );
}
