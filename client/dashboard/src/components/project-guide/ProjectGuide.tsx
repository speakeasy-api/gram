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
  const reducedMotion = useReducedMotion();
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
      <h3 className={expanded ? "hidden" : undefined}>
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

      {!expanded && !spine && <JourneyGraphic journey={journey} />}

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
    </motion.section>
  );
}

function JourneyGraphic({ journey }: { journey: JourneyMeta }): JSX.Element {
  const isA = journey.id === "third-party-mcp";
  const accent = isA ? "#2879D8" : "#B45A28";
  const plates = isA
    ? [
        ["Your client", "claude code · cursor · codex", "not connected"],
        ["Your endpoint", "tool access · tool logs", "not installed"],
        ["Upstream", "linear · vendor server", "not picked"],
      ]
    : [
        ["Your agent", "claude code + observability plugin", "no plugin"],
        ["Secrets policy", "deny on match", "off"],
        ["Model provider", "anthropic · openai", "unproven"],
      ];
  return (
    <div className="flex min-h-[400px] flex-col items-center justify-center px-6 pt-10 pb-8">
      <style>{`@keyframes guidepulse{0%,45%{transform:scale(1)}50%{transform:scale(1.018)}58%,100%{transform:scale(1)}}@keyframes tickOn{0%,45%{opacity:.16}55%,74%{opacity:1}100%{opacity:.16}}@media(prefers-reduced-motion:reduce){.motion-safe\\:animate-\\[guidepulse_9s_linear_infinite\\],.motion-safe\\:animate-\\[tickOn_2\\.4s_linear_infinite\\]{animation:none}}`}</style>
      {plates.map(([zone, name, status], index) => (
        <div key={zone} className="flex w-full flex-col items-center">
          <div
            className={cn(
              "relative flex w-[80%] flex-col gap-2 bg-[#FAFAFA] p-[13px_15px] shadow-[inset_0_0_0_1px_#DBDBDB]",
              index === 1 &&
                "w-full bg-[#F2F2F2] p-[17px_18px_15px_20px] shadow-[inset_0_0_0_1px_#DBDBDB] motion-safe:animate-[guidepulse_9s_linear_infinite]",
            )}
          >
            {index === 1 && (
              <span className="absolute inset-y-0 left-0 w-[5px] bg-[linear-gradient(180deg,#320F1E,#FA873C,#5A8250,#00143C,#9BC3FF)] opacity-40" />
            )}
            <div className="flex items-center gap-3 pl-0">
              <span className="flex min-w-0 flex-1 flex-col gap-1">
                <span className="font-mono text-[9.5px] tracking-[0.09em] text-[#121212]/42 uppercase">
                  {zone}
                </span>
                <span
                  className={cn(
                    "truncate text-[12.5px] leading-[1.2]",
                    index === 1 && "text-[18px]",
                  )}
                >
                  {name}
                </span>
              </span>
              <span className="shrink-0 font-mono text-[9.5px] text-[#121212]/40">
                {status}
              </span>
            </div>
            {index === 1 && (
              <div className="flex items-center gap-2 pl-2">
                <span className="flex flex-1 gap-[3px]">
                  {Array.from({ length: 12 }, (_, tick) => (
                    <span
                      key={tick}
                      className="h-[5px] flex-1 bg-[#B45A28] opacity-20 motion-safe:animate-[tickOn_2.4s_linear_infinite]"
                      style={{
                        animationDelay: `${-2.4 + tick * 0.2}s`,
                        backgroundColor: accent,
                      }}
                    />
                  ))}
                </span>
                <span className="font-mono text-[9px] tracking-[0.07em] text-[#121212]/45 uppercase">
                  {isA ? "in tool logs" : "1 risk event"}
                </span>
              </div>
            )}
          </div>
          {index < 2 && (
            <div className="relative h-[34px] w-3 border-x border-dashed border-[#C4C4C4]">
              <span className="absolute left-5 top-1/2 -translate-y-1/2 whitespace-nowrap font-mono text-[9.5px] tracking-[0.07em] text-[#121212]/35 uppercase">
                {isA
                  ? index === 0
                    ? "requests"
                    : "governed hop"
                  : index === 0
                    ? "every prompt"
                    : "denied here"}
              </span>
            </div>
          )}
        </div>
      ))}
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
