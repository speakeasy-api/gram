import {
  PROJECT_GUIDE_FIXTURES,
  type JourneyMeta,
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
  icon?: "play" | "pause";
  onClick?: () => void;
  disabled?: boolean;
  href?: string;
};

export type ProjectGuideRunProps = {
  journey: JourneyMeta;
  regionId: string;
  completionBody?: string;
  displayState: ProjectGuideDisplayState;
  completedSteps: number[];
  currentStep: number;
  currentContent: ReactNode;
  output: ReactNode;
  eventCard: ReactNode;
  primaryAction: ProjectGuideRunAction | null;
  onRewind?: (step: number) => void;
  onSwitchJourney: () => void;
};

function stepStateLabel(displayState: ProjectGuideDisplayState): string {
  switch (displayState) {
    case "running":
      return "running";
    case "preparing":
      return "preparing";
    case "waiting":
      return "listening";
    case "paused":
      return "paused";
    case "error":
      return "action needed";
    case "checkpoint":
    case "complete":
    case "opening":
    case "ready":
      return "waiting";
  }
}

export function ProjectGuideRun({
  journey,
  regionId,
  completionBody,
  displayState,
  completedSteps,
  currentStep,
  currentContent,
  output,
  eventCard,
  primaryAction,
  onRewind,
  onSwitchJourney,
}: ProjectGuideRunProps): JSX.Element {
  const reducedMotion = useReducedMotion();
  const fixture = PROJECT_GUIDE_FIXTURES[journey.id];
  const isComplete = displayState === "complete";
  const isEndStep = isComplete && currentStep === journey.steps.length;
  const resolvedCurrentContent = isEndStep ? (
    <CompletionStepBody
      journey={journey}
      body={completionBody}
      onSwitchJourney={onSwitchJourney}
    />
  ) : (
    currentContent
  );
  const activityLogRef = useRef<HTMLDivElement>(null);
  const stickToBottomRef = useRef(true);
  useEffect(() => {
    const activityLog = activityLogRef.current;
    if (activityLog && stickToBottomRef.current) {
      activityLog.scrollTop = activityLog.scrollHeight;
    }
  }, [output]);

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
        className="flex min-h-0 flex-col"
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
        <div className="relative min-w-0">
          <ol
            aria-label={`${journey.id === "third-party-mcp" ? "Journey A" : "Journey B"} steps`}
            className="min-w-0 lg:mr-[480px]"
          >
            {journey.steps.map((step, index) => {
              const complete = completedSteps.includes(index);
              const current = index === currentStep;
              return (
                <li
                  key={step}
                  aria-current={current ? "step" : undefined}
                  className={cn(
                    "border-neutral-softest min-w-0 border-b border-r-[3px] py-5 pl-6 pr-5",
                    current && "min-h-48",
                    current && "border-r-foreground",
                  )}
                  style={{
                    borderRightColor: complete ? fixture.accent : undefined,
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
            className="border-border bg-card flex min-w-0 flex-col border-l px-6 py-5 lg:absolute lg:inset-y-0 lg:right-0 lg:min-h-0 lg:w-[480px] lg:overflow-hidden"
          >
            <div className="flex items-baseline gap-3 px-4 py-3 border border-neutral-softest bg-muted/80">
              <span className="text-eyebrow" style={{ color: fixture.accent }}>
                {isEndStep ? "END" : String(currentStep + 1).padStart(2, "0")}
              </span>
              <h3 className="text-sm leading-tight">
                {journey.steps[currentStep] ?? "Journey complete"}
              </h3>
            </div>
            <div
              ref={activityLogRef}
              onScroll={() => {
                const activityLog = activityLogRef.current;
                if (!activityLog) return;
                stickToBottomRef.current =
                  activityLog.scrollHeight -
                    activityLog.scrollTop -
                    activityLog.clientHeight <
                  24;
              }}
              role="log"
              aria-label={`${journey.id === "third-party-mcp" ? "Journey A" : "Journey B"} activity`}
              aria-live="polite"
              className="border-neutral-softest bg-muted/30 flex min-h-0 max-h-[min(24rem,50dvh)] flex-1 flex-col gap-2 overflow-y-auto border p-4 lg:max-h-none"
            >
              <span className="text-eyebrow text-muted-foreground">
                Activity
              </span>
              <div className="font-mono text-xs leading-normal text-muted-foreground">
                {output}
                {eventCard}
              </div>
            </div>
            {primaryAction && (
              <div className="mt-auto grid gap-2">
                {primaryAction.href ? (
                  <Button href={primaryAction.href} className="w-full">
                    <Button.Text className="flex-none">
                      {primaryAction.label}
                    </Button.Text>
                  </Button>
                ) : (
                  <Button
                    type="button"
                    onClick={primaryAction.onClick}
                    disabled={primaryAction.disabled}
                    aria-label={primaryAction.label}
                    icon={primaryAction.icon}
                    iconAfter
                    className="w-full"
                  >
                    <Button.Text className="flex-none">
                      {primaryAction.label}
                    </Button.Text>
                  </Button>
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
  href,
}: {
  event: ProjectGuideEventCard;
  label: string;
  href?: string;
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
      {href && (
        <Link
          to={href}
          className="text-default-destructive text-eyebrow underline underline-offset-2"
        >
          View risk events
        </Link>
      )}
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
      <h4 className="text-display-xs">{journey.completion.heading}</h4>
      <p className="text-muted-foreground max-w-md text-body-sm">
        {body ?? journey.completion.body}
      </p>
      <div className="border-border mt-3 border-t pt-4">
        <Button type="button" onClick={onSwitchJourney} variant="secondary">
          Start the other journey
        </Button>
      </div>
    </div>
  );
}
