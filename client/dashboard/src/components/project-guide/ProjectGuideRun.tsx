import {
  PROJECT_GUIDE_FIXTURES,
  type JourneyMeta,
  type JourneyStatus,
} from "@/components/project-guide/journeys";
import type {
  ProjectGuideDisplayState,
  ProjectGuideEventCard,
} from "@/components/project-guide/projectGuideMachine";
import { cn } from "@/lib/utils";
import { motion, useReducedMotion } from "motion/react";
import { useEffect, useRef, type ReactNode } from "react";
import { Link } from "react-router";

export type ProjectGuideRunAction = {
  label: string;
  onClick?: () => void;
  disabled?: boolean;
  href?: string;
};

export type ProjectGuideRunProps = {
  journey: JourneyMeta;
  status: JourneyStatus;
  regionId: string;
  completionBody?: string;
  displayState?: ProjectGuideDisplayState;
  completedSteps?: number[];
  currentStep?: number;
  currentContent?: ReactNode;
  output?: ReactNode;
  eventCard?: ReactNode;
  primaryAction?: ProjectGuideRunAction | null;
  listeningElapsedSeconds?: number;
  onRewind?: (step: number) => void;
  onSwitchJourney: () => void;
};

function stepStateLabel(displayState: ProjectGuideDisplayState): string {
  switch (displayState) {
    case "running":
      return "running";
    case "waiting":
      return "listening";
    case "paused":
      return "paused";
    case "error":
      return "action needed";
    case "checkpoint":
    case "complete":
    case "exited":
    case "opening":
    case "ready":
      return "waiting";
  }
}

export function ProjectGuideRun({
  journey,
  status,
  regionId,
  completionBody,
  displayState: suppliedDisplayState,
  completedSteps: suppliedCompletedSteps,
  currentStep: suppliedCurrentStep,
  currentContent,
  output,
  eventCard,
  primaryAction,
  listeningElapsedSeconds = 0,
  onRewind,
  onSwitchJourney,
}: ProjectGuideRunProps): JSX.Element {
  const reducedMotion = useReducedMotion();
  const fixture = PROJECT_GUIDE_FIXTURES[journey.id];
  const displayState =
    suppliedDisplayState ??
    (status === "done"
      ? "complete"
      : status === "in-progress"
        ? "running"
        : "ready");
  const isComplete = displayState === "complete";
  const isRunning = displayState !== "ready";
  const currentStep = Math.min(
    suppliedCurrentStep ?? (status === "in-progress" ? 1 : 0),
    journey.steps.length - 1,
  );
  const completedSteps =
    suppliedCompletedSteps ??
    Array.from({ length: currentStep }, (_, index) => index);
  let resolvedCurrentContent = currentContent;
  let resolvedOutput = output;
  let resolvedEventCard = eventCard;
  let resolvedPrimaryAction = primaryAction;

  if (resolvedCurrentContent === undefined) {
    resolvedCurrentContent = (
      <p className="max-w-[52ch] pt-3 text-[13px] leading-[1.6] text-[#121212]/62">
        {journey.stepBlurbs[currentStep]}
      </p>
    );
  }
  if (resolvedOutput === undefined) {
    resolvedOutput = isRunning
      ? fixture.activity
      : "nothing has run for this step yet";
  }
  if (resolvedEventCard === undefined && (isRunning || isComplete)) {
    resolvedEventCard = (
      <ProjectGuideObservedEvent
        event={fixture.event}
        label={fixture.event.label}
      />
    );
  }
  if (resolvedPrimaryAction === undefined) {
    let label = "Start the journey";
    if (isRunning) label = "Watching for the event";
    if (isComplete) label = journey.completion.primaryAction;
    resolvedPrimaryAction = { label, disabled: !isComplete };
  }
  const resolvedSecondaryAction = isComplete
    ? {
        label: "Start the other journey",
        onClick: onSwitchJourney,
      }
    : undefined;
  const isStartAction =
    resolvedPrimaryAction !== null &&
    (displayState === "ready" || displayState === "checkpoint") &&
    resolvedPrimaryAction.label === "Start the journey";
  const isEnabledPrimaryAction =
    resolvedPrimaryAction !== null && !resolvedPrimaryAction.disabled;

  const activityLogRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const activityLog = activityLogRef.current;
    if (activityLog) activityLog.scrollTop = activityLog.scrollHeight;
  }, [resolvedOutput]);

  if (isComplete && resolvedPrimaryAction) {
    return (
      <section id={regionId} role="region" aria-label={journey.title}>
        <CompletionSummary
          journey={journey}
          body={completionBody}
          eventCard={resolvedEventCard}
          primaryAction={resolvedPrimaryAction}
          secondaryAction={resolvedSecondaryAction}
        />
      </section>
    );
  }

  return (
    <section
      id={regionId}
      role="region"
      aria-label={journey.title}
      className="min-w-0"
    >
      <motion.div
        initial={reducedMotion ? false : { opacity: 0, y: 8 }}
        animate={{ opacity: 1, y: 0 }}
        transition={
          reducedMotion
            ? { duration: 0 }
            : { duration: 0.3, ease: [0.2, 0.7, 0.3, 1] }
        }
        data-testid="project-guide-run"
        data-display-state={displayState}
      >
        <div className="flex items-center gap-3 border-b border-[#EBEBEB] bg-[#FAFAF9] px-[22px] py-[13px]">
          <span
            aria-hidden="true"
            className="size-[9px]"
            style={{ backgroundColor: fixture.accent }}
          />
          <span className="font-mono text-[10px] tracking-[0.08em] uppercase">
            Journey
          </span>
          <span className="font-mono text-[10px] tracking-[0.06em] text-[#121212]/35 uppercase">
            {fixture.meta}
          </span>
          <span
            className="ml-auto font-mono text-[9.5px] tracking-[0.06em] uppercase"
            style={{ color: fixture.accent }}
          >
            {completedSteps.length} of {journey.steps.length} done
          </span>
        </div>
        <div className="grid min-w-0 lg:grid-cols-[minmax(0,1fr)_352px]">
          <ol
            aria-label={`${journey.id === "third-party-mcp" ? "Journey A" : "Journey B"} steps`}
            className="min-w-0 overflow-hidden px-[22px] pt-2 pb-5"
          >
            {journey.steps.map((step, index) => {
              const complete = completedSteps.includes(index);
              const current = index === currentStep;
              return (
                <li
                  key={step}
                  aria-current={current ? "step" : undefined}
                  className="min-w-0 border-l-2 border-b border-[#F0EFED] py-3 pl-4"
                  style={{
                    borderLeftColor: complete
                      ? fixture.accent
                      : current
                        ? "#121212"
                        : "#EFEEEC",
                  }}
                >
                  <div className="flex items-baseline gap-3">
                    <span className="font-mono text-[10px] text-[#121212]/35">
                      {String(index + 1).padStart(2, "0")}
                    </span>
                    <span
                      className={cn(
                        "text-[13px] leading-[1.25]",
                        current && "text-[19px]",
                        !current && !complete && "text-[#121212]/45",
                      )}
                    >
                      {step}
                    </span>
                    {complete && onRewind ? (
                      <button
                        type="button"
                        onClick={() => onRewind(index)}
                        aria-label={`Rewind to ${step}`}
                        className="ml-auto font-mono text-[9.5px] tracking-[0.06em] text-[#121212]/40 uppercase"
                      >
                        redo
                      </button>
                    ) : (
                      <span className="ml-auto font-mono text-[9.5px] tracking-[0.06em] text-[#121212]/40 uppercase">
                        {complete
                          ? "done"
                          : current
                            ? stepStateLabel(displayState)
                            : ""}
                      </span>
                    )}
                  </div>
                  {current && resolvedCurrentContent}
                </li>
              );
            })}
          </ol>
          <aside
            aria-label={`${journey.id === "third-party-mcp" ? "Journey A" : "Journey B"} run panel`}
            className="flex min-h-[390px] min-w-0 flex-col gap-[13px] border-l border-[#EBEBEB] bg-[#FCFCFC] px-5 pt-[18px] pb-5"
          >
            <div className="flex items-baseline gap-2.5">
              <span
                className="font-mono text-[10px]"
                style={{ color: fixture.accent }}
              >
                {String(currentStep + 1).padStart(2, "0")}
              </span>
              <h3 className="text-[13px] leading-[1.3]">
                {journey.steps[currentStep]}
              </h3>
            </div>
            <span className="h-px bg-[#EBEBEB]" />
            <div
              ref={activityLogRef}
              role="log"
              aria-label={`${journey.id === "third-party-mcp" ? "Journey A" : "Journey B"} activity`}
              aria-live="polite"
              className="flex h-[180px] max-h-[180px] min-h-[120px] flex-none flex-col gap-2 overflow-y-auto bg-[#F7F7F6] p-3.5 shadow-[inset_0_0_0_1px_#E6E5E3]"
            >
              <span className="font-mono text-[9px] tracking-[0.09em] text-[#121212]/45 uppercase">
                Activity
              </span>
              <div className="font-mono text-[11px] leading-[1.5] text-[#121212]/55">
                {resolvedOutput}
              </div>
            </div>
            {displayState === "waiting" && (
              <div className="flex gap-1 font-mono text-[10px] text-[#121212]/50">
                <span role="status">Listening for an event</span>
                <span aria-hidden="true">·</span>
                <span aria-hidden="true">
                  {Math.floor(listeningElapsedSeconds)}s elapsed
                </span>
              </div>
            )}
            {resolvedEventCard}
            {resolvedPrimaryAction && (
              <button
                type="button"
                onClick={resolvedPrimaryAction.onClick}
                disabled={resolvedPrimaryAction.disabled}
                aria-label={resolvedPrimaryAction.label}
                className={cn(
                  "mt-auto flex w-full items-center justify-center gap-2 px-4 py-[11px] font-mono text-[11px] tracking-[0.06em] uppercase transition-colors",
                  isEnabledPrimaryAction
                    ? "bg-[#121212] text-[#FAFAFA]"
                    : "cursor-default bg-[#EDECEA] text-[#121212]/40",
                )}
              >
                {isStartAction && !resolvedPrimaryAction.disabled && (
                  <span
                    aria-hidden="true"
                    className="size-0 border-y-[5px] border-l-[8px] border-y-transparent border-l-current"
                  />
                )}
                {resolvedPrimaryAction.label}
              </button>
            )}
          </aside>
        </div>
      </motion.div>
    </section>
  );
}

export function ProjectGuideObservedEvent({
  event,
  label,
}: {
  event: ProjectGuideEventCard;
  label: string;
}): JSX.Element {
  const tone = event.tone === "deny" ? "#C83228" : "#5A8250";
  return (
    <section
      className="grid gap-1.5 border-l-2 p-2.5"
      style={{ borderLeftColor: tone }}
    >
      <span className="font-mono text-[9.5px] tracking-[0.09em] text-[#121212]/45 uppercase">
        {label}
      </span>
      <span
        className="font-mono text-[9px] tracking-[0.09em] uppercase"
        style={{ color: tone }}
      >
        {event.kind}
      </span>
      <span className="font-mono text-[11.5px]">{event.title}</span>
      {event.rows.map((row) => (
        <span key={row.key} className="flex gap-2 font-mono text-[10.5px]">
          <span className="w-16 shrink-0 text-[9.5px] tracking-[0.07em] text-[#121212]/42 uppercase">
            {row.key}
          </span>
          {row.value}
        </span>
      ))}
      <span className="border-t border-[#E6E5E3] pt-1.5 text-[11.5px] leading-[1.5] text-[#121212]/55">
        {event.note}
      </span>
    </section>
  );
}

function CompletionSummary({
  journey,
  body,
  eventCard,
  primaryAction,
  secondaryAction,
}: {
  journey: JourneyMeta;
  body?: string;
  eventCard: ReactNode;
  primaryAction: ProjectGuideRunAction;
  secondaryAction?: ProjectGuideRunAction;
}): JSX.Element {
  const fixture = PROJECT_GUIDE_FIXTURES[journey.id];
  return (
    <section className="grid gap-4 p-[22px]">
      <div className="flex items-center gap-3">
        <span
          className="font-mono text-[10px] tracking-[0.08em] uppercase"
          style={{ color: fixture.accent }}
        >
          {journey.completion.eyebrow}
        </span>
        <span
          className="h-px flex-1"
          style={{ backgroundColor: fixture.accent }}
        />
      </div>
      <h2 className="max-w-[24ch] font-display text-[32px] leading-[1] font-thin tracking-[-0.03em]">
        {journey.completion.heading}
      </h2>
      <p className="max-w-[56ch] text-[13px] leading-[1.6] text-[#121212]/62">
        {body ?? journey.completion.body}
      </p>
      {eventCard}
      <div className="flex flex-wrap gap-3">
        <CompletionPrimaryAction action={primaryAction} />
        {secondaryAction && (
          <button
            type="button"
            onClick={secondaryAction.onClick}
            disabled={secondaryAction.disabled}
            className="border border-[#DBDBDB] px-[18px] py-[10px] font-mono text-[11px] tracking-[0.06em] text-[#121212]/60 uppercase"
          >
            {secondaryAction.label}
          </button>
        )}
      </div>
    </section>
  );
}

function CompletionPrimaryAction({
  action,
}: {
  action: ProjectGuideRunAction;
}): JSX.Element {
  const className =
    "bg-[#121212] px-[18px] py-[11px] font-mono text-[11px] tracking-[0.06em] text-[#FAFAFA] uppercase";
  if (action.href) {
    return (
      <Link to={action.href} className={className}>
        {action.label}
      </Link>
    );
  }
  return (
    <button
      type="button"
      onClick={action.onClick}
      disabled={action.disabled}
      className={className}
    >
      {action.label}
    </button>
  );
}
