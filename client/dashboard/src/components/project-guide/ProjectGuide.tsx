import {
  BRAND_MESH_SURFACE_CLASS,
  BrandMeshLayers,
} from "@/components/brand-mesh";
import { ProjectGuideRun } from "@/components/project-guide/ProjectGuideRun";
import {
  JOURNEY_STATUS_LABELS,
  PROJECT_GUIDE_COMPLETE,
  PROJECT_GUIDE_FIXTURES,
  PROJECT_GUIDE_JOURNEYS,
  otherProjectGuideJourney,
  type JourneyId,
  type JourneyMeta,
  type JourneyStatus,
} from "@/components/project-guide/journeys";
import { useProjectGuideProgress } from "@/components/project-guide/useProjectGuideProgress";
import { cn } from "@/lib/utils";
import { motion, useReducedMotion } from "motion/react";
import { useCallback, useState } from "react";
import { useSearchParams } from "react-router";

/**
 * Fixture-backed project-home guide. Real journey operations are deliberately
 * not connected here; the existing progress result only selects display state.
 */
export function ProjectGuide(): JSX.Element {
  const { statusByJourney, isPending: progressPending } =
    useProjectGuideProgress();
  const [selected, setSelected] = useState<JourneyId | null>(null);
  const [reviewingCompletedJourneys, setReviewingCompletedJourneys] =
    useState(false);
  const [searchParams, setSearchParams] = useSearchParams();
  const reducedMotion = useReducedMotion();
  const selectedJourney = PROJECT_GUIDE_JOURNEYS.find(
    (journey) => journey.id === selected,
  );
  const selectedContentId = selected
    ? projectGuideContentId(selected)
    : undefined;
  const isComplete =
    statusByJourney["third-party-mcp"] === "done" &&
    statusByJourney["secret-block"] === "done";

  const returnToProjectHome = useCallback(() => {
    const nextSearchParams = new URLSearchParams(searchParams);
    nextSearchParams.delete("showGuide");
    setSearchParams(nextSearchParams, { replace: true });
  }, [searchParams, setSearchParams]);

  if (isComplete && !reviewingCompletedJourneys) {
    return (
      <GuideCanvas>
        <ProjectGuideComplete
          reducedMotion={reducedMotion}
          onReturnToProjectHome={returnToProjectHome}
          onReview={() => setReviewingCompletedJourneys(true)}
        />
      </GuideCanvas>
    );
  }

  return (
    <GuideCanvas>
      <section className="bg-card border-border mx-auto flex w-full max-w-[960px] flex-col overflow-hidden border shadow-[0_1px_2px_rgba(18,18,18,.04)]">
        <header className="flex items-baseline gap-3.5 border-b border-[#121212]/10 px-6 py-[18px] pb-[14px]">
          <h2 className="font-display text-[28px] leading-[.95] font-thin tracking-[-0.03em]">
            {selectedJourney?.title ?? "Put your agent traffic under control"}
          </h2>
          {selected && (
            <button
              type="button"
              onClick={() => setSelected(null)}
              aria-controls={selectedContentId}
              aria-expanded="true"
              className="ml-auto font-mono text-[10.5px] tracking-[0.05em] text-[#121212]/40 uppercase hover:text-[#121212]"
            >
              ← Back to start
            </button>
          )}
        </header>
        <div className="flex min-h-[400px] flex-col md:flex-row">
          {PROJECT_GUIDE_JOURNEYS.map((journey) => {
            const status = statusByJourney[journey.id];
            const isSelected = selected === journey.id;
            const isSpine = selected !== null && !isSelected;
            return (
              <motion.section
                key={journey.id}
                layout={!reducedMotion}
                transition={
                  reducedMotion
                    ? { duration: 0 }
                    : { layout: { duration: 0.5, ease: [0.65, 0, 0.25, 1] } }
                }
                data-testid={`project-guide-${journey.id}-card`}
                data-state={isSelected ? "open" : isSpine ? "spine" : "closed"}
                className={cn(
                  "min-w-0 overflow-hidden",
                  isSpine
                    ? "bg-[#F7F7F7] md:w-[54px] md:flex-none"
                    : "md:flex-1",
                  journey.id === "third-party-mcp" &&
                    "border-l border-[#121212]/10",
                )}
              >
                {isSpine ? (
                  <JourneySpine
                    journey={journey}
                    status={status}
                    controlsId={projectGuideContentId(journey.id)}
                    onSelect={() => setSelected(journey.id)}
                  />
                ) : isSelected ? (
                  <ProjectGuideRun
                    journey={journey}
                    status={status}
                    regionId={projectGuideContentId(journey.id)}
                    onSwitchJourney={() =>
                      setSelected(otherProjectGuideJourney(journey.id))
                    }
                  />
                ) : (
                  <JourneyChoice
                    journey={journey}
                    status={status}
                    statusPending={progressPending}
                    controlsId={projectGuideContentId(journey.id)}
                    onSelect={() => setSelected(journey.id)}
                  />
                )}
              </motion.section>
            );
          })}
        </div>
      </section>
    </GuideCanvas>
  );
}

function projectGuideContentId(journeyId: JourneyId): string {
  return `project-guide-${journeyId}-content`;
}

function GuideCanvas({ children }: { children: React.ReactNode }): JSX.Element {
  return (
    <div
      className={cn(
        BRAND_MESH_SURFACE_CLASS,
        "relative flex min-h-[calc(100dvh-var(--header-height))] w-full p-4 sm:p-8",
      )}
    >
      <BrandMeshLayers />
      <div className="relative z-10 flex w-full items-start justify-center">
        {children}
      </div>
    </div>
  );
}

function JourneyChoice({
  journey,
  status,
  statusPending,
  controlsId,
  onSelect,
}: {
  journey: JourneyMeta;
  status: JourneyStatus;
  statusPending: boolean;
  controlsId: string;
  onSelect: () => void;
}): JSX.Element {
  const fixture = PROJECT_GUIDE_FIXTURES[journey.id];
  const isComplete = status === "done";
  const isInProgress = status === "in-progress";
  const isUnavailable = status === "unreadable";
  const statusLabel = statusPending ? "Loading" : JOURNEY_STATUS_LABELS[status];
  const progressLabel = isUnavailable
    ? "Progress unavailable"
    : isComplete
      ? "Complete"
      : isInProgress
        ? "1 of 5 done"
        : "Not started";

  return (
    <button
      type="button"
      onClick={onSelect}
      aria-label={journey.title}
      aria-controls={controlsId}
      aria-expanded="false"
      className="flex h-full w-full flex-col text-left"
    >
      <JourneyGraphic journey={journey} status={status} />
      <span className="flex flex-col gap-2 border-t border-[#121212]/10 bg-[#FAFAFA] p-[22px] transition-[background,box-shadow] hover:bg-card hover:shadow-[inset_0_-3px_0_#121212]">
        <span className="flex items-center gap-2.5">
          {journey.steps.map((step, index) => (
            <span
              key={step}
              aria-hidden="true"
              className="h-[3px] w-4"
              style={{
                backgroundColor:
                  isComplete || (isInProgress && index === 0)
                    ? fixture.accent
                    : "#DCDCDC",
              }}
            />
          ))}
          <span className="font-mono text-[9.5px] tracking-[0.07em] text-[#121212]/40 uppercase">
            {progressLabel}
          </span>
        </span>
        <span className="text-[18px] leading-[1.2]">{journey.title}</span>
        <span className="text-[12.5px] leading-[1.55] text-[#121212]/62">
          {journey.win}
        </span>
        <span className="flex items-center gap-3 pt-1">
          <span className="font-mono text-[11.5px]">
            {isComplete
              ? "Review"
              : isInProgress
                ? "Resume the run"
                : "Open the journey"}
          </span>
          <span
            className="h-px flex-1"
            style={{ backgroundColor: fixture.accent }}
          />
          <span className="font-mono text-[11px] text-[#121212]/40">
            5 steps · ~4 min
          </span>
          <span
            className="font-mono text-[11.5px]"
            style={{ color: fixture.accent }}
          >
            →
          </span>
        </span>
        <span className="sr-only">{statusLabel}</span>
      </span>
    </button>
  );
}

function JourneyGraphic({
  journey,
  status,
}: {
  journey: JourneyMeta;
  status: JourneyStatus;
}): JSX.Element {
  const fixture = PROJECT_GUIDE_FIXTURES[journey.id];
  const reducedMotion = useReducedMotion();
  const isMcp = journey.id === "third-party-mcp";
  const plates = isMcp
    ? [
        [
          "Your client",
          "claude code · cursor · codex",
          "connected",
          "not connected",
        ],
        [
          "Your endpoint",
          "tool access · tool logs",
          "verified",
          "not installed",
        ],
        ["Upstream", "linear · vendor server", "27 tools", "not picked"],
      ]
    : [
        [
          "Your agent",
          "claude code + observability plugin",
          "streaming",
          "no plugin",
        ],
        ["Secrets policy", "deny on match", "enforcing", "off"],
        [
          "Model provider",
          "anthropic · openai",
          "unsafe prompt not received",
          "unproven",
        ],
      ];
  const active = status !== "not-started" && status !== "unreadable";

  return (
    <motion.span
      animate={
        reducedMotion || status !== "not-started"
          ? undefined
          : { opacity: [0.9, 1, 0.9] }
      }
      transition={{ duration: 9, ease: "linear", repeat: Infinity }}
      className="flex min-h-[400px] flex-col items-center justify-center px-6 pt-10 pb-8"
    >
      {plates.map(([zone, name, onStatus, offStatus], index) => (
        <span key={zone} className="flex w-full flex-col items-center">
          <span
            className={cn(
              "relative flex w-4/5 flex-col gap-2 bg-[#FAFAFA] p-[13px_15px] shadow-[inset_0_0_0_1px_#DBDBDB]",
              index === 1 && "w-full bg-[#F2F2F2] p-[17px_18px_15px_20px]",
            )}
          >
            {index === 1 && (
              <span
                aria-hidden="true"
                className="absolute inset-y-0 left-0 w-[5px] opacity-40"
                style={{
                  background:
                    "linear-gradient(180deg,#320F1E,#FA873C,#5A8250,#00143C,#9BC3FF)",
                }}
              />
            )}
            <span className="flex items-center gap-3">
              <span className="flex min-w-0 flex-1 flex-col gap-1">
                <span className="font-mono text-[9.5px] tracking-[0.09em] text-[#121212]/42 uppercase">
                  {zone}
                </span>
                <span
                  className={cn(
                    "truncate text-[12.5px]",
                    index === 1 && "text-[18px]",
                  )}
                >
                  {name}
                </span>
              </span>
              <span className="font-mono text-[9.5px] text-[#121212]/40">
                {active ? onStatus : offStatus}
              </span>
            </span>
            {index === 1 && (
              <span className="flex gap-[3px] pl-2">
                {Array.from({ length: 12 }, (_, tick) => (
                  <span
                    key={tick}
                    aria-hidden="true"
                    className="h-[5px] flex-1"
                    style={{
                      backgroundColor: fixture.accent,
                      opacity: active ? 1 : 0.16,
                    }}
                  />
                ))}
              </span>
            )}
          </span>
          {index < 2 && (
            <span className="relative h-[34px] w-3 border-x border-dashed border-[#C4C4C4]">
              <span className="absolute left-5 top-1/2 -translate-y-1/2 whitespace-nowrap font-mono text-[9.5px] tracking-[0.07em] text-[#121212]/35 uppercase">
                {isMcp
                  ? index === 0
                    ? "requests"
                    : "governed hop"
                  : index === 0
                    ? "every prompt"
                    : "denied here"}
              </span>
            </span>
          )}
        </span>
      ))}
    </motion.span>
  );
}

function JourneySpine({
  journey,
  status,
  controlsId,
  onSelect,
}: {
  journey: JourneyMeta;
  status: JourneyStatus;
  controlsId: string;
  onSelect: () => void;
}): JSX.Element {
  const fixture = PROJECT_GUIDE_FIXTURES[journey.id];
  const meta =
    status === "done"
      ? "complete"
      : status === "in-progress"
        ? "1 / 5"
        : fixture.meta;
  return (
    <button
      type="button"
      onClick={onSelect}
      aria-label={`Switch to ${journey.title}`}
      aria-controls={controlsId}
      aria-expanded="false"
      className="flex h-full w-full flex-col items-center gap-3.5 py-5 hover:bg-card/60"
    >
      <span
        aria-hidden="true"
        className="size-2"
        style={{ backgroundColor: fixture.accent }}
      />
      <span className="flex-1 [writing-mode:vertical-rl] font-mono text-[10.5px] tracking-[0.08em] text-[#121212]/50 uppercase">
        Journey · {meta}
      </span>
      <span
        aria-hidden="true"
        className="font-mono text-[11px] text-[#121212]/35"
      >
        ↔
      </span>
    </button>
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
      transition={
        reducedMotion
          ? { duration: 0 }
          : { duration: 0.4, ease: [0.2, 0.7, 0.3, 1] }
      }
      className="bg-card border-border grid w-full max-w-[960px] gap-4 border p-6"
    >
      <span className="text-eyebrow text-primary">
        {PROJECT_GUIDE_COMPLETE.eyebrow}
      </span>
      <h2 className="max-w-[24ch] font-display text-[32px] leading-[1.05] font-thin tracking-[-0.03em]">
        {PROJECT_GUIDE_COMPLETE.heading}
      </h2>
      <p className="max-w-[56ch] text-[13px] leading-[1.6] text-muted-foreground">
        {PROJECT_GUIDE_COMPLETE.body}
      </p>
      <div className="flex flex-wrap items-center gap-3">
        <button
          type="button"
          onClick={onReturnToProjectHome}
          className="bg-foreground px-4 py-2 font-mono text-[11px] tracking-[0.05em] text-background uppercase"
        >
          {PROJECT_GUIDE_COMPLETE.primaryAction}
        </button>
        <button
          type="button"
          onClick={onReview}
          className="border border-[#DBDBDB] px-4 py-2 font-mono text-[11px] tracking-[0.05em] text-[#121212]/60 uppercase"
        >
          {PROJECT_GUIDE_COMPLETE.secondaryAction}
        </button>
      </div>
      <span className="font-mono text-[10px] text-muted-foreground uppercase">
        {PROJECT_GUIDE_COMPLETE.note}
      </span>
    </motion.section>
  );
}
