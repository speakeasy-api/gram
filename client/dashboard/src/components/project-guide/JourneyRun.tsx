import {
  projectGuideMachine,
  type ProjectGuideEvent,
  type ProjectGuideEventCard,
  type ProjectGuideStepKind,
} from "@/components/project-guide/projectGuideMachine";
import type {
  JourneyId,
  JourneyStatus,
} from "@/components/project-guide/journeys";
import { cn } from "@/lib/utils";
import { useMachine } from "@xstate/react";
import { motion, useReducedMotion } from "motion/react";
import {
  createContext,
  useContext,
  useEffect,
  useRef,
  type ReactNode,
} from "react";
import type { SnapshotFrom } from "xstate";

type RunTone = "information" | "destructive";

const STEP_STATUS = {
  done: "done",
  running: "running",
  waiting: "waiting",
  await: "your turn",
  error: "action needed",
} as const;

type ProjectGuideRunContextValue = {
  snapshot: SnapshotFrom<typeof projectGuideMachine>;
  send: (event: ProjectGuideEvent) => void;
};

const ProjectGuideRunContext =
  createContext<ProjectGuideRunContextValue | null>(null);

// The hook shares the actor with journey-specific controls; isolated journey
// tests may still render without the provider.
// eslint-disable-next-line react-refresh/only-export-components
export function useProjectGuideRun(): ProjectGuideRunContextValue | null {
  return useContext(ProjectGuideRunContext);
}

export function ProjectGuideRunProvider({
  journeyId,
  currentStep,
  children,
}: {
  journeyId: JourneyId;
  currentStep: number;
  children: ReactNode;
}): JSX.Element {
  const stepKinds: ProjectGuideStepKind[] =
    journeyId === "third-party-mcp"
      ? ["pick", "phase", "tabs", "prompt", "listen"]
      : ["phase", "phase", "tabs", "prompt", "listen"];
  const [snapshot, send] = useMachine(projectGuideMachine, {
    input: {
      initialStep: Math.min(Math.max(currentStep, 0), 4),
      completed: Array.from({ length: Math.max(currentStep, 0) }, (_, i) => i),
      stepKinds,
    },
  });
  return (
    <ProjectGuideRunContext.Provider value={{ snapshot, send }}>
      {children}
    </ProjectGuideRunContext.Provider>
  );
}

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
  completionEvent,
  completionPrimary,
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
  completionEvent?: ProjectGuideEventCard | null;
  completionPrimary?: ReactNode;
  children: ReactNode;
}): JSX.Element {
  const shouldReduceMotion = useReducedMotion();
  const stepKinds: ProjectGuideStepKind[] =
    journeyId === "third-party-mcp"
      ? ["pick", "phase", "tabs", "prompt", "listen"]
      : ["phase", "phase", "tabs", "prompt", "listen"];
  const completed = Array.from(
    { length: Math.min(Math.max(currentStep, 0), steps.length) },
    (_, i) => i,
  );
  const [snapshot, send] = useMachine(projectGuideMachine, {
    input: {
      initialStep: Math.min(Math.max(currentStep, 0), steps.length - 1),
      completed,
      stepKinds,
    },
  });
  const parentRun = useProjectGuideRun();
  const activeSnapshot = parentRun?.snapshot ?? snapshot;
  const activeSend = parentRun?.send ?? send;
  const logRef = useRef<HTMLDivElement>(null);
  const phase = complete ? "complete" : error ? "error" : activeSnapshot.value;
  const activeStep = Math.min(activeSnapshot.context.step, steps.length - 1);
  const accent = tone === "information" ? "#2879D8" : "#B45A28";
  const journeyLabel =
    journeyId === "third-party-mcp" ? "Journey A" : "Journey B";
  const isListening = phase === "waiting";
  const fallbackRun =
    !parentRun &&
    Boolean(onPause && !paused && currentStep >= 2 && !complete && !error);
  const isRunning = phase === "running" || phase === "waiting" || fallbackRun;
  const isError = phase === "error";
  const isComplete = phase === "complete";
  const isPaused = activeSnapshot.value === "paused" || paused;

  useEffect(() => {
    if (!fallbackRun || activeSnapshot.value === "waiting") return;
    activeSend({ type: "LISTEN" });
  }, [activeSend, activeSnapshot.value, fallbackRun]);

  useEffect(() => {
    const node = logRef.current;
    if (node) node.scrollTop = node.scrollHeight;
  }, [activeSnapshot.context.logs.length, activeSnapshot.context.event]);

  useEffect(() => {
    if (error)
      activeSend({
        type: "FAIL",
        message: "The project check needs attention.",
      });
  }, [error, activeSend]);

  useEffect(() => {
    if (!isListening) return;
    const timer = setInterval(() => activeSend({ type: "TICK" }), 100);
    return () => clearInterval(timer);
  }, [isListening, activeSend]);

  const action = (event: ProjectGuideEvent) => activeSend(event);
  const eventCard = activeSnapshot.context.event;

  return (
    <ProjectGuideRunContext.Provider
      value={{ snapshot: activeSnapshot, send: activeSend }}
    >
      <style>{`@keyframes jline{from{opacity:0;transform:translateY(4px)}to{opacity:1;transform:translateY(0)}}@keyframes jrises{from{opacity:0;transform:translateY(8px)}to{opacity:1;transform:translateY(0)}}@media(prefers-reduced-motion:reduce){.motion-safe\\:animate-\\[jline_.22s_both\\]{animation:none!important}}`}</style>
      <motion.section
        initial={shouldReduceMotion ? false : { opacity: 0, y: 8 }}
        animate={{ opacity: 1, y: 0 }}
        transition={
          shouldReduceMotion
            ? { duration: 0 }
            : { duration: 0.3, ease: [0.2, 0.7, 0.3, 1] }
        }
        className="min-w-0"
        data-testid="journey-run"
        data-phase={phase}
      >
        <div className="flex items-center gap-3 border-b border-[#EBEBEB] bg-[#FAFAF9] px-[22px] py-[13px]">
          <span
            className="size-[9px] shrink-0"
            style={{ backgroundColor: accent }}
          />
          <span className="font-mono text-[10px] tracking-[0.08em] text-[#121212] uppercase">
            Journey
          </span>
          <span className="font-mono text-[10px] tracking-[0.06em] text-[#121212]/35 uppercase">
            {journeyId === "third-party-mcp"
              ? "govern a third-party MCP"
              : "block a leaked credential"}
          </span>
          <span
            className="ml-auto font-mono text-[9.5px] tracking-[0.06em] uppercase"
            style={{ color: accent }}
          >
            {activeSnapshot.context.completed.length} of 5 done
          </span>
          <button type="button" onClick={onSwitchJourney} className="sr-only">
            Switch journey
          </button>
        </div>

        <div className="grid min-w-0 lg:grid-cols-[minmax(0,1fr)_352px]">
          <ol
            aria-label={`${journeyLabel} steps`}
            className="min-w-0 px-[22px] pt-2 pb-5"
          >
            {steps.map((step, index) => {
              const done = activeSnapshot.context.completed.includes(index);
              const current = !done && index === activeStep && !isComplete;
              const rowStatus = done
                ? status === "in-progress" && index < currentStep
                  ? "restored"
                  : "done"
                : current
                  ? (STEP_STATUS[phase as keyof typeof STEP_STATUS] ?? "")
                  : "";
              return (
                <li
                  key={step}
                  className="border-b border-[#F0EFED] border-l-2 px-0 py-3 pl-4 transition-[border-color,padding] duration-300"
                  style={{
                    borderLeftColor: done
                      ? accent
                      : current
                        ? "#121212"
                        : "#EFEEEC",
                  }}
                  data-current={current ? "true" : undefined}
                  aria-current={current ? "step" : undefined}
                >
                  <button
                    type="button"
                    disabled={!done}
                    onClick={() => action({ type: "REWIND", step: index })}
                    className={cn(
                      "flex w-full items-baseline gap-3 text-left",
                      !done && "cursor-default",
                    )}
                  >
                    <span className="font-mono text-[10px] text-[#121212]/35">
                      {String(index + 1).padStart(2, "0")}
                    </span>
                    <span
                      className={cn(
                        "text-[13px] leading-[1.25]",
                        current && "text-[19px]",
                        !done && !current && "text-[#121212]/45",
                      )}
                    >
                      {step}
                    </span>
                    <span className="ml-auto shrink-0 font-mono text-[9.5px] tracking-[0.06em] text-[#121212]/40 uppercase">
                      {rowStatus}
                    </span>
                  </button>
                  {current && (
                    <motion.div
                      initial={
                        shouldReduceMotion ? false : { opacity: 0, y: 8 }
                      }
                      animate={{ opacity: 1, y: 0 }}
                      transition={
                        shouldReduceMotion
                          ? { duration: 0 }
                          : { duration: 0.34, ease: [0.2, 0.7, 0.3, 1] }
                      }
                      className="pt-4"
                    >
                      {children}
                    </motion.div>
                  )}
                </li>
              );
            })}
            {isComplete && (
              <CompletionSummary
                journeyId={journeyId}
                accent={accent}
                onSwitchJourney={onSwitchJourney}
                event={completionEvent}
                primaryAction={completionPrimary}
              />
            )}
          </ol>

          {!isComplete && (
            <aside className="flex min-h-[390px] min-w-0 flex-col gap-[13px] border-l border-[#EBEBEB] bg-[#FCFCFC] px-5 pt-[18px] pb-5">
              <div className="flex items-baseline gap-2.5">
                <span
                  className="font-mono text-[10px]"
                  style={{ color: accent }}
                >
                  {String(activeStep + 1).padStart(2, "0")}
                </span>
                <h4 className="min-w-0 text-[13px] leading-[1.3]">
                  {title || steps[activeStep]}
                </h4>
              </div>
              <span className="h-px bg-[#EBEBEB]" />
              <div
                ref={logRef}
                role="log"
                aria-label={`${journeyLabel} activity`}
                aria-live="polite"
                className="flex min-h-[120px] flex-1 flex-col gap-1.5 overflow-y-auto bg-[#F7F7F6] p-3.5 shadow-[inset_0_0_0_1px_#E6E5E3]"
              >
                <div className="sticky top-[-14px] flex items-center gap-2 bg-[#F7F7F6] py-3">
                  <span
                    className={cn("size-[5px]", isRunning && "animate-pulse")}
                    style={{
                      backgroundColor: isError
                        ? "#C83228"
                        : isRunning
                          ? "#121212"
                          : accent,
                    }}
                  />
                  <span className="font-mono text-[9px] tracking-[0.09em] text-[#121212]/45 uppercase">
                    {isListening ? "listening" : "activity"}
                  </span>
                  {isListening && (
                    <span className="ml-auto font-mono text-[9px] text-[#121212]/40">
                      +{activeSnapshot.context.elapsed.toFixed(1)}s
                    </span>
                  )}
                </div>
                {activeSnapshot.context.logs.length === 0 && !eventCard && (
                  <span className="font-mono text-[11px] leading-[1.5] text-[#121212]/40">
                    {isListening
                      ? "nothing yet · this pane fills when the event arrives"
                      : "nothing has run for this step yet"}
                  </span>
                )}
                {activeSnapshot.context.logs.slice(-18).map((log, index) => (
                  <span
                    key={`${log}-${index}`}
                    className="motion-safe:animate-[jline_.22s_both] font-mono text-[11px] leading-[1.5]"
                  >
                    {log}
                  </span>
                ))}
                {eventCard && <EventCard event={eventCard} />}
                {isRunning && (
                  <span className="mt-0.5 flex items-center gap-2 font-mono text-[11px] text-[#121212]/55">
                    <span className="size-[13px] rounded-full bg-[conic-gradient(from_0deg,#320F1E,#FA873C,#5A8250,#00143C,#9BC3FF,#320F1E)] motion-safe:animate-spin" />
                    <span>{isListening ? "listening…" : "working…"}</span>
                  </span>
                )}
              </div>
              <button
                type="button"
                disabled={phase === "await"}
                aria-label={
                  isRunning
                    ? "Pause live checks"
                    : isPaused
                      ? "Resume live checks"
                      : isError
                        ? "Retry and continue"
                        : status === "in-progress" || currentStep > 0
                          ? "Resume the journey"
                          : "Start the journey"
                }
                onClick={() => {
                  if (isError) return action({ type: "RETRY" });
                  if (isRunning) {
                    onPause?.();
                    return action({ type: "PAUSE" });
                  }
                  if (isPaused) {
                    onResume?.();
                    return action({ type: "RESUME" });
                  }
                  return action({ type: "START" });
                }}
                className={cn(
                  "flex w-full items-center justify-center gap-2 px-4 py-[11px] font-mono text-[11px] tracking-[0.06em] uppercase transition-colors",
                  isError
                    ? "bg-[#C83228] text-white"
                    : isRunning
                      ? "border border-[#DBDBDB] bg-transparent text-[#121212]/70"
                      : phase === "await"
                        ? "cursor-default bg-[#EDECEA] text-[#121212]/40"
                        : "bg-[#121212] text-[#FAFAFA]",
                )}
              >
                {isRunning ? (
                  "Pause"
                ) : isError ? (
                  journeyId === "third-party-mcp" && activeStep === 4 ? (
                    "Listen again"
                  ) : (
                    "Retry and continue"
                  )
                ) : isPaused ? (
                  <>
                    <PlayIcon />
                    Resume the journey
                  </>
                ) : status === "in-progress" || currentStep > 0 ? (
                  <>
                    <PlayIcon />
                    Resume the journey
                  </>
                ) : (
                  <>
                    <PlayIcon />
                    Start the journey
                  </>
                )}
              </button>
              {isError && (
                <button
                  type="button"
                  onClick={() => action({ type: "RESET" })}
                  className="w-full border border-[#DBDBDB] px-4 py-[9px] font-mono text-[10.5px] tracking-[0.06em] text-[#121212]/55 uppercase"
                >
                  Back to start
                </button>
              )}
            </aside>
          )}
        </div>
      </motion.section>
    </ProjectGuideRunContext.Provider>
  );
}

function PlayIcon(): JSX.Element {
  return (
    <span
      aria-hidden="true"
      className="size-0 border-y-[5px] border-l-[8px] border-y-transparent border-l-current"
    />
  );
}

function EventCard({
  event,
  completed = false,
}: {
  event: ProjectGuideEventCard;
  completed?: boolean;
}): JSX.Element {
  const tone = event.tone === "deny" ? "#C83228" : "#5A8250";
  return (
    <div
      className="mt-0.5 grid gap-1.5 border-l-2 p-2.5"
      style={{
        borderLeftColor: tone,
        backgroundColor: event.tone === "deny" ? "#FBF3F2" : "#F3F7F2",
      }}
    >
      <div className="flex items-baseline gap-2">
        <span
          className="font-mono text-[9px] tracking-[0.09em] uppercase"
          style={{ color: tone }}
        >
          {event.kind}
        </span>
        <span className="ml-auto font-mono text-[9px] text-[#121212]/40">
          {completed ? "kept on the record" : "just now"}
        </span>
      </div>
      <span className="font-mono text-[11.5px] leading-[1.45]">
        {event.title}
      </span>
      {event.tone === "deny" && (
        <span className="font-mono text-[11px]">Blocked by secrets policy</span>
      )}
      {event.rows.map((row) => (
        <span key={row.key} className="flex items-baseline gap-2">
          <span className="w-16 shrink-0 font-mono text-[9.5px] tracking-[0.07em] text-[#121212]/42 uppercase">
            {row.key}
          </span>
          <span className="font-mono text-[10.5px] leading-[1.45]">
            {row.value}
          </span>
        </span>
      ))}
      {event.tone === "deny" ? (
        <>
          <span className="border-t border-[#E6E5E3] pt-1.5 text-[11.5px] leading-[1.5] text-[#121212]/55">
            The request was blocked before the model answered.
          </span>
          <span className="text-[11.5px] leading-[1.5] text-[#121212]/55">
            {event.note}
          </span>
        </>
      ) : (
        <span className="border-t border-[#E6E5E3] pt-1.5 text-[11.5px] leading-[1.5] text-[#121212]/55">
          {event.note}
        </span>
      )}
    </div>
  );
}

function CompletionSummary({
  journeyId,
  accent,
  onSwitchJourney,
  event,
  primaryAction,
}: {
  journeyId: JourneyId;
  accent: string;
  onSwitchJourney: () => void;
  event?: ProjectGuideEventCard | null;
  primaryAction?: ReactNode;
}): JSX.Element {
  const isA = journeyId === "third-party-mcp";
  return (
    <div className="grid gap-3 px-0 pt-8 pb-4 animate-[jrises_.4s_cubic-bezier(.2,.7,.3,1)_both]">
      <span
        className="font-mono text-[10px] tracking-[0.08em] uppercase"
        style={{ color: accent }}
      >
        {isA ? "Journey A complete" : "Journey B complete"}
      </span>
      <h3 className="max-w-[24ch] font-display text-[32px] leading-[.95] font-thin tracking-[-0.03em]">
        {isA ? "The path is governed." : "The prompt was denied."}
      </h3>
      <p className="max-w-[56ch] text-[13px] leading-[1.6] text-[#121212]/62">
        {isA
          ? "Your client now reaches a third-party MCP through an endpoint you own. Calls land in tool logs with the actor, tools, and result attached."
          : "The prompt matched the secrets policy and was rejected before the model answered. The finding sits in Risk Events with the rule that fired and who sent it."}
      </p>
      <div>
        <span className="font-mono text-[9px] tracking-[0.09em] text-[#121212]/45 uppercase">
          {isA ? "The call you watched" : "The event you watched"}
        </span>
        {event ? (
          <EventCard event={event} completed />
        ) : (
          <div
            className="border-l-2 p-2.5"
            style={{
              borderLeftColor: accent,
              backgroundColor: isA ? "#F3F7F2" : "#FBF3F2",
            }}
          >
            <p className="mt-1 font-mono text-[11px]">kept on the record</p>
          </div>
        )}
      </div>
      <div className="flex flex-wrap gap-2">
        {primaryAction ?? (
          <button
            type="button"
            className="bg-[#121212] px-[18px] py-[11px] font-mono text-[11px] tracking-[0.06em] text-[#FAFAFA] uppercase"
          >
            {isA ? "Open tool logs" : "Open Risk Events"}
          </button>
        )}
        <button
          type="button"
          onClick={onSwitchJourney}
          className="border border-[#DBDBDB] px-[18px] py-[10px] font-mono text-[11px] tracking-[0.06em] text-[#121212]/60 uppercase"
        >
          Start the other journey
        </button>
      </div>
    </div>
  );
}
