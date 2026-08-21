import {
  PROJECT_GUIDE_FIXTURES,
  type JourneyMeta,
  type JourneyStatus,
} from "@/components/project-guide/journeys";
import { Button } from "@/components/ui/Button";
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

function journeyActionIcon(label: string): "play" | "pause" | undefined {
  if (label === "Start the journey") {
    return "play";
  }
  if (
    label === "Pause the journey" ||
    label === "Pause listening" ||
    label === "Pause"
  ) {
    return "pause";
  }
  return undefined;
}

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
  const currentStep = isComplete
    ? journey.steps.length
    : Math.min(
        suppliedCurrentStep ?? (status === "in-progress" ? 1 : 0),
        journey.steps.length - 1,
      );
  const isEndStep = isComplete && currentStep === journey.steps.length;
  const completedSteps =
    suppliedCompletedSteps ??
    Array.from({ length: currentStep }, (_, index) => index);
  let resolvedCurrentContent = isEndStep ? (
    <CompletionStepBody
      journey={journey}
      body={completionBody}
      onSwitchJourney={onSwitchJourney}
    />
  ) : (
    currentContent
  );
  let resolvedOutput = output;
  let resolvedEventCard = eventCard;
  let resolvedPrimaryAction = primaryAction;

  if (resolvedCurrentContent === undefined) {
    resolvedCurrentContent = (
      <p className="max-w-md pt-3 text-body-sm text-muted-foreground">
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
  const activityLogRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const activityLog = activityLogRef.current;
    if (activityLog) activityLog.scrollTop = activityLog.scrollHeight;
  }, [resolvedOutput]);

  return (
    <section
      id={regionId}
      role="region"
      aria-label={journey.title}
      className="h-full min-w-0"
    >
      <motion.div
        initial={reducedMotion ? false : { opacity: 0, y: 8 }}
        animate={{ opacity: 1, y: 0 }}
        transition={
          reducedMotion
            ? { duration: 0 }
            : { duration: 0.3, ease: [0.2, 0.7, 0.3, 1] }
        }
        className="flex h-full min-h-0 flex-col"
        data-testid="project-guide-run"
        data-display-state={displayState}
      >
        <div className="border-border bg-background flex items-center gap-3 border-b px-6 py-3">
          <span
            aria-hidden="true"
            className="size-2.5"
            style={{ backgroundColor: fixture.accent }}
          />
          <span className="text-eyebrow">Journey</span>
          <span className="text-eyebrow text-disabled">{fixture.meta}</span>
          <span
            className="text-eyebrow ml-auto"
            style={{ color: fixture.accent }}
          >
            {completedSteps.length} of {journey.steps.length} done
          </span>
        </div>
        <div className="flex min-h-0 min-w-0 flex-1 flex-col overflow-y-auto lg:flex-row lg:overflow-hidden">
          <ol
            aria-label={`${journey.id === "third-party-mcp" ? "Journey A" : "Journey B"} steps`}
            className="min-w-0 overflow-hidden px-6 pt-2 pb-5 lg:flex-1 lg:overflow-y-auto"
          >
            {journey.steps.map((step, index) => {
              const complete = completedSteps.includes(index);
              const current = index === currentStep;
              return (
                <li
                  key={step}
                  aria-current={current ? "step" : undefined}
                  className={cn(
                    "border-neutral-softest min-w-0 border-b border-l-2 py-3 pl-4",
                    current && "border-l-foreground",
                  )}
                  style={{
                    borderLeftColor: complete ? fixture.accent : undefined,
                  }}
                >
                  <div className="flex items-baseline gap-3">
                    <span className="text-eyebrow text-disabled">
                      {String(index + 1).padStart(2, "0")}
                    </span>
                    <span
                      className={cn(
                        "text-sm leading-tight",
                        current && "text-xl",
                        !current && !complete && "text-muted-foreground",
                      )}
                    >
                      {step}
                    </span>
                    {complete && onRewind ? (
                      <button
                        type="button"
                        onClick={() => onRewind(index)}
                        aria-label={`Rewind to ${step}`}
                        className="text-eyebrow text-disabled ml-auto"
                      >
                        redo
                      </button>
                    ) : (
                      <span className="text-eyebrow text-disabled ml-auto">
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
            {isEndStep && (
              <li
                aria-current="step"
                className="border-neutral-softest min-w-0 border-b border-l-2 py-3 pl-4"
                style={{ borderLeftColor: fixture.accent }}
              >
                <div className="flex items-baseline gap-3">
                  <span className="text-eyebrow text-disabled">END</span>
                  <span className="text-xl leading-tight">
                    Journey complete
                  </span>
                  <span className="text-eyebrow text-disabled ml-auto">
                    done
                  </span>
                </div>
                {resolvedCurrentContent}
              </li>
            )}
          </ol>
          <aside
            aria-label={`${journey.id === "third-party-mcp" ? "Journey A" : "Journey B"} run panel`}
            className="border-border bg-card flex min-h-96 min-w-0 flex-col gap-3 border-l px-5 pt-5 pb-5 lg:min-h-0 lg:w-2/5 lg:overflow-y-auto"
          >
            <div className="flex items-baseline gap-2.5">
              <span className="text-eyebrow" style={{ color: fixture.accent }}>
                {isEndStep ? "END" : String(currentStep + 1).padStart(2, "0")}
              </span>
              <h3 className="text-sm leading-tight">
                {journey.steps[currentStep] ?? "Journey complete"}
              </h3>
            </div>
            <span className="bg-border h-px" />
            <div
              ref={activityLogRef}
              role="log"
              aria-label={`${journey.id === "third-party-mcp" ? "Journey A" : "Journey B"} activity`}
              aria-live="polite"
              className="border-neutral-softest bg-muted/40 flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto border p-4"
            >
              <span className="text-eyebrow text-muted-foreground">
                Activity
              </span>
              <div className="font-mono text-xs leading-normal text-muted-foreground">
                {resolvedOutput}
                {resolvedEventCard}
              </div>
              {displayState === "waiting" && (
                <div className="text-muted-foreground flex gap-1 font-mono text-xs">
                  <span role="status">Listening for an event</span>
                  <span aria-hidden="true">·</span>
                  <span aria-hidden="true">
                    {Math.floor(listeningElapsedSeconds)}s elapsed
                  </span>
                </div>
              )}
            </div>
            {resolvedPrimaryAction && (
              <div className="mt-auto grid gap-2">
                {resolvedPrimaryAction?.href ? (
                  <Button asChild className="w-full">
                    <Link to={resolvedPrimaryAction.href}>
                      <Button.Text className="flex-none">
                        {resolvedPrimaryAction.label}
                      </Button.Text>
                    </Link>
                  </Button>
                ) : (
                  resolvedPrimaryAction && (
                    <Button
                      type="button"
                      onClick={resolvedPrimaryAction.onClick}
                      disabled={resolvedPrimaryAction.disabled}
                      aria-label={resolvedPrimaryAction.label}
                      icon={journeyActionIcon(resolvedPrimaryAction.label)}
                      iconAfter
                      className="w-full"
                    >
                      <Button.Text className="flex-none">
                        {resolvedPrimaryAction.label}
                      </Button.Text>
                    </Button>
                  )
                )}
              </div>
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
  const toneClasses =
    event.tone === "deny"
      ? {
          border: "border-destructive-default",
          text: "text-default-destructive",
        }
      : { border: "border-success-default", text: "text-default-success" };
  return (
    <section
      className={cn("grid gap-1.5 border-l-2 p-2.5", toneClasses.border)}
    >
      <span className="text-eyebrow text-muted-foreground">{label}</span>
      <span className={cn("text-eyebrow", toneClasses.text)}>{event.kind}</span>
      <span className="font-mono text-sm">{event.title}</span>
      {event.rows.map((row) => (
        <span key={row.key} className="flex gap-2 font-mono text-xs">
          <span className="text-eyebrow text-disabled w-16 shrink-0">
            {row.key}
          </span>
          {row.value}
        </span>
      ))}
      <span className="border-neutral-softest text-muted-foreground border-t pt-1.5 text-sm leading-normal">
        {event.note}
      </span>
    </section>
  );
}

function CompletionStepBody({
  journey,
  body,
  onSwitchJourney,
}: {
  journey: JourneyMeta;
  body?: string;
  onSwitchJourney: () => void;
}): JSX.Element {
  return (
    <div className="grid gap-3 pt-3">
      <h4 className="text-display-xs max-w-48">{journey.completion.heading}</h4>
      <p className="text-muted-foreground max-w-md text-body-sm">
        {body ?? journey.completion.body}
      </p>
      <div className="border-border mt-3 border-t pt-4">
        <button
          type="button"
          onClick={onSwitchJourney}
          className="border-border text-muted-foreground w-fit border px-4 py-2 font-mono text-xs uppercase"
        >
          Start the other journey
        </button>
      </div>
    </div>
  );
}
