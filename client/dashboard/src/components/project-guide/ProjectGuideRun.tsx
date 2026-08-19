import {
  PROJECT_GUIDE_FIXTURES,
  type JourneyId,
  type JourneyMeta,
  type JourneyStatus,
} from "@/components/project-guide/journeys";
import { cn } from "@/lib/utils";
import { motion, useReducedMotion } from "motion/react";
import type { ReactNode } from "react";

export type ProjectGuideRunAction = {
  label: string;
  onClick?: () => void;
  disabled?: boolean;
};

export type ProjectGuideRunProps = {
  journey: JourneyMeta;
  status: JourneyStatus;
  regionId: string;
  currentStep?: number;
  currentContent?: ReactNode;
  output?: ReactNode;
  eventCard?: ReactNode;
  primaryAction?: ProjectGuideRunAction;
  secondaryAction?: ProjectGuideRunAction;
  onSwitchJourney: () => void;
};

export function ProjectGuideRun({
  journey,
  status,
  regionId,
  currentStep: suppliedCurrentStep,
  currentContent,
  output,
  eventCard,
  primaryAction,
  secondaryAction,
  onSwitchJourney,
}: ProjectGuideRunProps): JSX.Element {
  const reducedMotion = useReducedMotion();
  const fixture = PROJECT_GUIDE_FIXTURES[journey.id];
  const isComplete = status === "done";
  const isRunning = status === "in-progress";
  const currentStep = suppliedCurrentStep ?? (isRunning ? 1 : 0);
  let resolvedCurrentContent = currentContent;
  let resolvedOutput = output;
  let resolvedEventCard = eventCard;
  let resolvedPrimaryAction = primaryAction;
  let resolvedSecondaryAction = secondaryAction;

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
    resolvedEventCard = <EventCard journeyId={journey.id} />;
  }
  if (resolvedPrimaryAction === undefined) {
    let label = "Start the journey";
    if (isRunning) label = "Watching for the event";
    if (isComplete) label = journey.completion.primaryAction;
    resolvedPrimaryAction = { label, disabled: !isComplete };
  }
  if (resolvedSecondaryAction === undefined && isComplete) {
    resolvedSecondaryAction = {
      label: "Start the other journey",
      onClick: onSwitchJourney,
    };
  }

  if (isComplete) {
    return (
      <section id={regionId} role="region" aria-label={journey.title}>
        <CompletionSummary
          journey={journey}
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
            {isRunning ? "1 of 5 done" : "0 of 5 done"}
          </span>
        </div>
        <div className="grid min-w-0 lg:grid-cols-[minmax(0,1fr)_352px]">
          <ol
            aria-label={`${journey.id === "third-party-mcp" ? "Journey A" : "Journey B"} steps`}
            className="min-w-0 px-[22px] pt-2 pb-5"
          >
            {journey.steps.map((step, index) => {
              const complete = isRunning && index === 0;
              const current = index === currentStep;
              return (
                <li
                  key={step}
                  aria-current={current ? "step" : undefined}
                  className="border-l-2 border-b border-[#F0EFED] py-3 pl-4"
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
                    <span className="ml-auto font-mono text-[9.5px] tracking-[0.06em] text-[#121212]/40 uppercase">
                      {complete
                        ? "done"
                        : current
                          ? isRunning
                            ? "running"
                            : "waiting"
                          : ""}
                    </span>
                  </div>
                  {current && resolvedCurrentContent}
                </li>
              );
            })}
          </ol>
          <aside className="flex min-h-[390px] min-w-0 flex-col gap-[13px] border-l border-[#EBEBEB] bg-[#FCFCFC] px-5 pt-[18px] pb-5">
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
              role="log"
              aria-label={`${journey.id === "third-party-mcp" ? "Journey A" : "Journey B"} activity`}
              aria-live="polite"
              className="flex min-h-[120px] flex-1 flex-col gap-2 bg-[#F7F7F6] p-3.5 shadow-[inset_0_0_0_1px_#E6E5E3]"
            >
              <span className="font-mono text-[9px] tracking-[0.09em] text-[#121212]/45 uppercase">
                Activity
              </span>
              <div className="font-mono text-[11px] leading-[1.5] text-[#121212]/55">
                {resolvedOutput}
              </div>
            </div>
            {resolvedEventCard}
            <button
              type="button"
              onClick={resolvedPrimaryAction.onClick}
              disabled={resolvedPrimaryAction.disabled}
              className="w-full cursor-default bg-[#EDECEA] px-4 py-[11px] font-mono text-[11px] tracking-[0.06em] text-[#121212]/40 uppercase"
            >
              {resolvedPrimaryAction.label}
            </button>
            {resolvedSecondaryAction && (
              <button
                type="button"
                onClick={resolvedSecondaryAction.onClick}
                disabled={resolvedSecondaryAction.disabled}
                className="w-full border border-[#DBDBDB] px-4 py-[10px] font-mono text-[11px] tracking-[0.06em] text-[#121212]/60 uppercase"
              >
                {resolvedSecondaryAction.label}
              </button>
            )}
          </aside>
        </div>
      </motion.div>
    </section>
  );
}

function EventCard({ journeyId }: { journeyId: JourneyId }): JSX.Element {
  const fixture = PROJECT_GUIDE_FIXTURES[journeyId];
  const tone = fixture.event.tone === "deny" ? "#C83228" : "#5A8250";
  return (
    <section
      className="grid gap-1.5 border-l-2 p-2.5"
      style={{ borderLeftColor: tone }}
    >
      <span className="font-mono text-[9.5px] tracking-[0.09em] text-[#121212]/45 uppercase">
        {fixture.event.label}
      </span>
      <span
        className="font-mono text-[9px] tracking-[0.09em] uppercase"
        style={{ color: tone }}
      >
        {fixture.event.kind}
      </span>
      <span className="font-mono text-[11.5px]">{fixture.event.title}</span>
      {fixture.event.rows.map((row) => (
        <span key={row.key} className="flex gap-2 font-mono text-[10.5px]">
          <span className="w-16 shrink-0 text-[9.5px] tracking-[0.07em] text-[#121212]/42 uppercase">
            {row.key}
          </span>
          {row.value}
        </span>
      ))}
      <span className="border-t border-[#E6E5E3] pt-1.5 text-[11.5px] leading-[1.5] text-[#121212]/55">
        {fixture.event.note}
      </span>
    </section>
  );
}

function CompletionSummary({
  journey,
  eventCard,
  primaryAction,
  secondaryAction,
}: {
  journey: JourneyMeta;
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
        {journey.completion.body}
      </p>
      {eventCard}
      <div className="flex flex-wrap gap-3">
        <button
          type="button"
          onClick={primaryAction.onClick}
          disabled={primaryAction.disabled}
          className="bg-[#121212] px-[18px] py-[11px] font-mono text-[11px] tracking-[0.06em] text-[#FAFAFA] uppercase"
        >
          {primaryAction.label}
        </button>
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
