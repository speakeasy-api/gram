import type {
  JourneyId,
  JourneyStatus,
} from "@/components/project-guide/journeys";
import { cn } from "@/lib/utils";
import { motion, useReducedMotion } from "motion/react";
import { useEffect, useState, type ReactNode } from "react";

type RunTone = "information" | "destructive";

export function JourneyRun({
  journeyId,
  steps,
  currentStep,
  status,
  title,
  tone,
  error = false,
  complete = false,
  paused = false,
  onPause,
  onResume,
  onSwitchJourney,
  children,
}: {
  journeyId: JourneyId;
  steps: string[];
  currentStep: number;
  status: JourneyStatus;
  title: string;
  tone: RunTone;
  error?: boolean;
  complete?: boolean;
  paused?: boolean;
  onPause?: () => void;
  onResume?: () => void;
  onSwitchJourney: () => void;
  children: ReactNode;
}): JSX.Element {
  const shouldReduceMotion = useReducedMotion();
  const [started, setStarted] = useState(
    () => status !== "not-started" || currentStep > 0,
  );

  useEffect(() => {
    if (status !== "not-started" || currentStep > 0) setStarted(true);
  }, [currentStep, status]);

  const runState = complete
    ? "complete"
    : error
      ? "error"
      : paused
        ? "paused"
        : started
          ? "running"
          : "ready";
  const journeyLabel =
    journeyId === "third-party-mcp" ? "Journey A" : "Journey B";
  const boundedStep = Math.min(Math.max(currentStep, 0), steps.length - 1);

  return (
    <motion.section
      initial={shouldReduceMotion ? false : { opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      transition={
        shouldReduceMotion
          ? { duration: 0 }
          : { duration: 0.34, ease: [0.2, 0.7, 0.3, 1] }
      }
      className={cn(
        "border-border overflow-hidden border-l-2",
        tone === "information"
          ? "border-l-information-default"
          : "border-l-destructive",
      )}
    >
      <div className="border-border flex items-center justify-between gap-4 border-b px-4 py-3">
        <div className="flex items-center gap-3">
          <span className="font-mono text-[10px] tracking-[0.08em] uppercase">
            {journeyLabel}
          </span>
          <span className="text-muted-foreground font-mono text-[10px] tracking-[0.05em] uppercase">
            Step {complete ? steps.length : boundedStep + 1} of {steps.length}
          </span>
        </div>
        <button
          type="button"
          onClick={onSwitchJourney}
          className="text-muted-foreground font-mono text-[10px] tracking-[0.05em] uppercase"
        >
          Switch journey
        </button>
      </div>

      <div className="grid lg:grid-cols-[minmax(220px,1fr)_minmax(300px,352px)]">
        <ol
          aria-label={`${journeyLabel} steps`}
          className="divide-border border-border divide-y border-b lg:border-r lg:border-b-0"
        >
          {steps.map((step, index) => {
            const stepComplete = complete || index < boundedStep;
            const active = !complete && index === boundedStep;
            return (
              <motion.li
                key={step}
                initial={shouldReduceMotion ? false : { opacity: 0, x: -4 }}
                animate={{ opacity: 1, x: 0 }}
                transition={
                  shouldReduceMotion
                    ? { duration: 0 }
                    : { duration: 0.2, delay: index * 0.035 }
                }
                aria-current={active ? "step" : undefined}
                className={cn(
                  "grid min-h-14 grid-cols-[28px_1fr_auto] items-center gap-3 px-4 py-3",
                  active && "bg-muted/40",
                )}
              >
                <span
                  aria-hidden="true"
                  className={cn(
                    "border-border flex size-7 items-center justify-center border font-mono text-[10px]",
                    stepComplete &&
                      "border-foreground bg-foreground text-background",
                    active && !stepComplete && "border-foreground",
                  )}
                >
                  {stepComplete ? "✓" : String(index + 1).padStart(2, "0")}
                </span>
                <span
                  className={cn(
                    "text-[13px] leading-[1.4]",
                    !active && !stepComplete && "text-muted-foreground",
                  )}
                >
                  {step}
                </span>
                <span className="text-muted-foreground font-mono text-[9px] tracking-[0.06em] uppercase">
                  {stepComplete ? "Done" : active ? "Now" : "Next"}
                </span>
              </motion.li>
            );
          })}
        </ol>

        <div className="bg-muted/15 min-w-0 p-4">
          <div className="grid gap-4">
            <div className="flex items-start justify-between gap-3">
              <div className="grid gap-1">
                <span className="text-muted-foreground font-mono text-[9px] tracking-[0.08em] uppercase">
                  Run panel
                </span>
                <h4 className="text-[19px] leading-[1.2]">{title}</h4>
              </div>
              <span
                className={cn(
                  "border-border border px-1.5 py-px font-mono text-[9px] tracking-[0.08em] uppercase",
                  runState === "error" && "text-destructive",
                  runState === "complete" && "text-success-default",
                )}
              >
                {runState}
              </span>
            </div>

            <div
              role="log"
              aria-label={`${journeyLabel} activity`}
              aria-live="polite"
              className="border-border bg-card grid gap-2 border p-3"
            >
              <span className="text-muted-foreground font-mono text-[9px] tracking-[0.08em] uppercase">
                Activity log
              </span>
              <p className="font-mono text-[10px] leading-[1.5]">
                {runState === "ready" &&
                  "Ready to begin from current project state."}
                {runState === "running" && `Current · ${steps[boundedStep]}`}
                {runState === "paused" &&
                  "Live checks paused; project state is unchanged."}
                {runState === "error" &&
                  "Run needs attention. Retry the failed check below."}
                {runState === "complete" &&
                  "Journey complete. The watched event is recorded."}
              </p>
            </div>

            <div className="flex flex-wrap gap-2">
              {!started && !complete && (
                <button
                  type="button"
                  onClick={() => setStarted(true)}
                  className="bg-foreground text-background px-3 py-2 font-mono text-[10px] tracking-[0.05em] uppercase"
                >
                  Start the journey
                </button>
              )}
              {started && !complete && !error && onPause && !paused && (
                <button
                  type="button"
                  onClick={onPause}
                  className="border-border border px-3 py-2 font-mono text-[10px] tracking-[0.05em] uppercase"
                >
                  Pause live checks
                </button>
              )}
              {paused && onResume && (
                <button
                  type="button"
                  onClick={onResume}
                  className="border-border border px-3 py-2 font-mono text-[10px] tracking-[0.05em] uppercase"
                >
                  Resume live checks
                </button>
              )}
            </div>

            <div className="grid min-w-0 gap-4">{children}</div>
          </div>
        </div>
      </div>
    </motion.section>
  );
}
