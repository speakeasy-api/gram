import { firstIncompleteStepIndex } from "@/components/project-guide/journeyStatus";
import { SecretBlockJourney } from "@/components/project-guide/SecretBlockJourney";
import { ThirdPartyMcpJourney } from "@/components/project-guide/ThirdPartyMcpJourney";
import {
  PROJECT_GUIDE_JOURNEYS,
  JOURNEY_STATUS_LABELS,
  otherProjectGuideJourney,
  type JourneyId,
  type JourneyMeta,
  type JourneyStatus,
} from "@/components/project-guide/journeys";
import { useProjectGuideProgress } from "@/components/project-guide/useProjectGuideProgress";
import { cn } from "@/lib/utils";
import { invalidateAllGetMcpServerActivity } from "@gram/client/react-query/getMcpServerActivity.js";
import { invalidateAllRiskListResults } from "@gram/client/react-query/riskListResults.js";
import { useQueryClient } from "@tanstack/react-query";
import { motion, useReducedMotion } from "motion/react";
import { useCallback, useState } from "react";
import { useSearchParams } from "react-router";

/**
 * The zero-data guide that takes project home's space when the gate opens it.
 * Two journeys, each ending in something the user watches happen. The cards are
 * an accordion: no overlay, no drawer, no navigation away.
 */
export function ProjectGuide(): JSX.Element {
  const { statusByJourney, isPending: progressPending } =
    useProjectGuideProgress();
  const [expanded, setExpanded] = useState<JourneyId | null>(null);
  const [searchParams, setSearchParams] = useSearchParams();
  const queryClient = useQueryClient();
  const shouldReduceMotion = useReducedMotion();
  const isComplete =
    statusByJourney["third-party-mcp"] === "done" &&
    statusByJourney["secret-block"] === "done";

  const toggle = useCallback((id: JourneyId) => {
    setExpanded((current) => (current === id ? null : id));
  }, []);
  const switchJourney = useCallback((id: JourneyId) => {
    setExpanded(id);
  }, []);
  const markJourneyComplete = useCallback(
    (id: JourneyId) => {
      if (id === "third-party-mcp") {
        void invalidateAllGetMcpServerActivity(queryClient);
      } else {
        void invalidateAllRiskListResults(queryClient);
      }
      setExpanded(otherProjectGuideJourney(id));
    },
    [queryClient],
  );
  const returnToProjectHome = useCallback(() => {
    const nextSearchParams = new URLSearchParams(searchParams);
    nextSearchParams.delete("showGuide");
    setSearchParams(nextSearchParams, { replace: true });
  }, [searchParams, setSearchParams]);

  if (isComplete) {
    return (
      <ProjectGuideComplete
        reducedMotion={shouldReduceMotion}
        onReturnToProjectHome={returnToProjectHome}
      />
    );
  }

  const selectedJourney = PROJECT_GUIDE_JOURNEYS.find(
    (journey) => journey.id === expanded,
  );

  return (
    <div className="w-full pt-2 pb-6">
      <div className="flex items-start justify-between gap-4 pb-6">
        <div className="flex flex-col gap-2">
          <span className="text-eyebrow">Journey</span>
          <h2 className="text-foreground font-display text-[32px] leading-[1.05] font-thin tracking-[-0.03em]">
            {selectedJourney?.title ?? "Put your agent traffic under control"}
          </h2>
          <p className="text-muted-foreground max-w-[62ch] text-[13px] leading-[1.6]">
            Choose a journey to govern a third-party MCP or block a synthetic
            credential before it reaches a model.
          </p>
        </div>
        {selectedJourney && (
          <button
            type="button"
            onClick={() => setExpanded(null)}
            className="text-muted-foreground font-mono text-[10px] tracking-[0.05em] uppercase"
          >
            ← Back to start
          </button>
        )}
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
            onComplete={() => markJourneyComplete(journey.id)}
            onSwitchJourney={() =>
              switchJourney(otherProjectGuideJourney(journey.id))
            }
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
  onComplete,
  onSwitchJourney,
}: {
  journey: JourneyMeta;
  status: JourneyStatus;
  statusPending: boolean;
  expanded: boolean;
  onToggle: () => void;
  onComplete: () => void;
  onSwitchJourney: () => void;
}): JSX.Element {
  const currentStep = firstIncompleteStepIndex(status, journey.steps.length);
  const reducedMotion = useReducedMotion();
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
        <motion.div
          id={panelId}
          role="region"
          aria-labelledby={triggerId}
          data-testid="journey-body"
          initial={reducedMotion ? false : { opacity: 0, y: 8 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{
            duration: reducedMotion ? 0 : 0.3,
            ease: [0.2, 0.7, 0.3, 1],
          }}
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
          {journey.id === "third-party-mcp" ? (
            <ThirdPartyMcpJourney
              status={status}
              onComplete={onComplete}
              onSwitchJourney={onSwitchJourney}
            />
          ) : (
            <SecretBlockJourney
              status={status}
              onComplete={onComplete}
              onSwitchJourney={onSwitchJourney}
            />
          )}
        </motion.div>
      )}
    </section>
  );
}

function ProjectGuideComplete({
  reducedMotion,
  onReturnToProjectHome,
}: {
  reducedMotion: boolean | null;
  onReturnToProjectHome: () => void;
}): JSX.Element {
  return (
    <motion.section
      data-testid="project-guide-complete"
      initial={reducedMotion ? false : { opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{
        duration: reducedMotion ? 0 : 0.4,
        ease: [0.2, 0.7, 0.3, 1],
      }}
      className="bg-card border-border grid gap-4 border p-6.5"
    >
      <span className="text-eyebrow text-primary">Both journeys complete</span>
      <h2 className="text-foreground max-w-[24ch] font-display text-[32px] leading-[1.05] font-thin tracking-[-0.03em]">
        Both journeys are on the record.
      </h2>
      <p className="text-muted-foreground max-w-[56ch] text-[13px] leading-[1.6]">
        Traffic is governed and recorded, and prompts are inspected before
        transport. Return to project home to see the live project data.
      </p>
      <button
        type="button"
        onClick={onReturnToProjectHome}
        className="bg-foreground text-background w-fit px-4 py-2 font-mono text-[11px] tracking-[0.05em] uppercase"
      >
        Go to project home
      </button>
    </motion.section>
  );
}
