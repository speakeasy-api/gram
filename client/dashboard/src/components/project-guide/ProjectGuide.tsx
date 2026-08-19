import { firstIncompleteStepIndex } from "@/components/project-guide/journeyStatus";
import { SecretBlockJourney } from "@/components/project-guide/SecretBlockJourney";
import { ThirdPartyMcpJourney } from "@/components/project-guide/ThirdPartyMcpJourney";
import {
  PROJECT_GUIDE_COMPLETE,
  PROJECT_GUIDE_JOURNEYS,
  JOURNEY_STATUS_LABELS,
  otherProjectGuideJourney,
  type JourneyId,
  type JourneyMeta,
  type JourneyStatus,
} from "@/components/project-guide/journeys";
import { useProjectGuideProgress } from "@/components/project-guide/useProjectGuideProgress";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { invalidateGetMcpServerActivity } from "@gram/client/react-query/getMcpServerActivity.js";
import { invalidateRiskListResults } from "@gram/client/react-query/riskListResults.js";
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
  const [reviewingCompletedJourneys, setReviewingCompletedJourneys] =
    useState(false);
  const [searchParams, setSearchParams] = useSearchParams();
  const queryClient = useQueryClient();
  const gramProject = useProjectSlugForRequests();
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
        void invalidateGetMcpServerActivity(queryClient, [{ gramProject }]);
      } else {
        void invalidateRiskListResults(queryClient, [{ gramProject }]);
      }
      setExpanded(id);
    },
    [gramProject, queryClient],
  );
  const returnToProjectHome = useCallback(() => {
    const nextSearchParams = new URLSearchParams(searchParams);
    nextSearchParams.delete("showGuide");
    setSearchParams(nextSearchParams, { replace: true });
  }, [searchParams, setSearchParams]);

  if (isComplete && !reviewingCompletedJourneys) {
    return (
      <ProjectGuideComplete
        reducedMotion={shouldReduceMotion}
        onReturnToProjectHome={returnToProjectHome}
        onReview={() => setReviewingCompletedJourneys(true)}
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

      <div className="bg-card border-border flex flex-col overflow-hidden border md:flex-row">
        {PROJECT_GUIDE_JOURNEYS.map((journey) => (
          <JourneyCard
            key={journey.id}
            journey={journey}
            status={statusByJourney[journey.id]}
            statusPending={progressPending}
            expanded={expanded === journey.id}
            spine={expanded !== null && expanded !== journey.id}
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
  spine,
  onToggle,
  onComplete,
  onSwitchJourney,
}: {
  journey: JourneyMeta;
  status: JourneyStatus;
  statusPending: boolean;
  expanded: boolean;
  spine: boolean;
  onToggle: () => void;
  onComplete: () => void;
  onSwitchJourney: () => void;
}): JSX.Element {
  const currentStep = firstIncompleteStepIndex(status, journey.steps.length);
  const reducedMotion = useReducedMotion();
  const routes = useRoutes();
  const triggerId = `project-guide-${journey.id}-trigger`;
  const panelId = `project-guide-${journey.id}-panel`;

  return (
    <motion.section
      layout={!reducedMotion}
      transition={
        reducedMotion
          ? { duration: 0 }
          : { layout: { duration: 0.5, ease: [0.65, 0, 0.25, 1] } }
      }
      data-testid={`project-guide-${journey.id}-card`}
      data-state={expanded ? "open" : spine ? "spine" : "closed"}
      className={cn(
        "min-w-0 overflow-hidden",
        expanded || !spine ? "md:flex-1" : "md:w-[54px] md:flex-none",
        journey.id === "third-party-mcp" && "border-border md:border-l",
      )}
    >
      <h3>
        <button
          id={triggerId}
          type="button"
          onClick={onToggle}
          aria-controls={panelId}
          aria-expanded={expanded}
          aria-label={spine ? `Switch to ${journey.title} journey` : undefined}
          className={cn(
            "flex w-full flex-col items-start gap-2 px-6.5 py-5 text-left",
            spine && "md:h-full md:w-[54px] md:items-center md:px-2 md:py-4",
          )}
        >
          <span
            className={cn(
              "flex w-full items-center gap-2.5",
              spine && "md:flex-col",
            )}
          >
            <span
              className={cn(
                "text-muted-foreground font-mono text-xs tracking-wider",
                spine && "md:hidden",
              )}
            >
              {journey.index}
            </span>
            <span
              className={cn(
                "text-foreground text-[19px] leading-[1.25]",
                spine &&
                  "md:[writing-mode:vertical-rl] md:rotate-180 md:whitespace-nowrap",
              )}
            >
              {journey.title}
            </span>
            <span
              role={statusPending ? "status" : undefined}
              aria-label={
                statusPending
                  ? `Loading ${journey.title} journey status`
                  : undefined
              }
              className={cn(
                "border-border text-muted-foreground ml-auto border px-1.5 py-px font-mono text-[9px] tracking-[0.08em] uppercase",
                spine && "md:ml-0",
              )}
            >
              {statusPending ? "Loading" : JOURNEY_STATUS_LABELS[status]}
            </span>
          </span>
          <span
            className={cn(
              "text-muted-foreground max-w-[64ch] text-[13px] leading-[1.6]",
              spine && "md:hidden",
            )}
          >
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
          {status === "done" ? (
            <JourneyCompleteSummary
              journey={journey}
              onSwitchJourney={onSwitchJourney}
              routes={routes}
            />
          ) : (
            <>
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
            </>
          )}
        </motion.div>
      )}
    </motion.section>
  );
}

function JourneyCompleteSummary({
  journey,
  onSwitchJourney,
  routes,
}: {
  journey: JourneyMeta;
  onSwitchJourney: () => void;
  routes: ReturnType<typeof useRoutes>;
}): JSX.Element {
  const completion = journey.completion;

  return (
    <div className="grid gap-4">
      <div className="grid gap-1">
        <span className="text-primary font-mono text-[10px] tracking-[0.05em] uppercase">
          {completion.eyebrow}
        </span>
        <h4 className="text-[24px] leading-[1.1]">{completion.heading}</h4>
        <p className="text-muted-foreground max-w-[52ch] text-[13px] leading-[1.6]">
          {completion.body}
        </p>
      </div>
      <div className="flex flex-wrap items-center gap-4">
        {journey.id === "third-party-mcp" ? (
          <routes.logs.Link className="font-mono text-[11px] uppercase">
            {completion.primaryAction}
          </routes.logs.Link>
        ) : (
          <routes.riskEvents.Link className="font-mono text-[11px] uppercase">
            {completion.primaryAction}
          </routes.riskEvents.Link>
        )}
        <button
          type="button"
          onClick={onSwitchJourney}
          className="text-muted-foreground font-mono text-[11px] uppercase"
        >
          Start the other journey
        </button>
      </div>
    </div>
  );
}

function ProjectGuideComplete({
  reducedMotion,
  onReturnToProjectHome,
  onReview,
}: {
  reducedMotion: boolean | null;
  onReturnToProjectHome: () => void;
  onReview: () => void;
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
      <span className="text-eyebrow text-primary">
        {PROJECT_GUIDE_COMPLETE.eyebrow}
      </span>
      <h2 className="text-foreground max-w-[24ch] font-display text-[32px] leading-[1.05] font-thin tracking-[-0.03em]">
        {PROJECT_GUIDE_COMPLETE.heading}
      </h2>
      <p className="text-muted-foreground max-w-[56ch] text-[13px] leading-[1.6]">
        {PROJECT_GUIDE_COMPLETE.body}
      </p>
      <div className="flex flex-wrap items-center gap-4">
        <button
          type="button"
          onClick={onReturnToProjectHome}
          className="bg-foreground text-background w-fit px-4 py-2 font-mono text-[11px] tracking-[0.05em] uppercase"
        >
          {PROJECT_GUIDE_COMPLETE.primaryAction}
        </button>
        <button
          type="button"
          onClick={onReview}
          className="text-muted-foreground font-mono text-[11px] uppercase"
        >
          {PROJECT_GUIDE_COMPLETE.secondaryAction}
        </button>
      </div>
      <span className="text-muted-foreground font-mono text-[10px] uppercase">
        {PROJECT_GUIDE_COMPLETE.note}
      </span>
    </motion.section>
  );
}
