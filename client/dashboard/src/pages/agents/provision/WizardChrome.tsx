import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import { Check } from "lucide-react";
import type { JSX, ReactNode } from "react";

/**
 * The frame every provisioning step shares: the five-cell stepper, the body,
 * the summary rail, and one footer that carries the note and the moves.
 *
 * The steps own their content and nothing else — a step that drew its own
 * heading, rail or buttons is what made the old wizard read as five unrelated
 * screens.
 */

export type WizardStepState = "done" | "current" | "todo";

export function WizardStepper({
  steps,
  current,
  forward = false,
  onJump,
}: {
  steps: string[];
  current: number;
  /** Whether a step ahead of the current one can be jumped to. */
  forward?: boolean;
  onJump: (index: number) => void;
}): JSX.Element {
  return (
    <ol
      aria-label="Provisioning steps"
      className="border-border grid grid-cols-2 border md:grid-cols-5"
    >
      {steps.map((label, index) => {
        const state: WizardStepState =
          index < current ? "done" : index === current ? "current" : "todo";
        return (
          <li key={label} className="border-border border-r last:border-r-0">
            <button
              type="button"
              aria-current={state === "current" ? "step" : undefined}
              // A step ahead is a destination only where nothing later is
              // built from a choice not yet made.
              disabled={state === "current" || (state === "todo" && !forward)}
              onClick={() => onJump(index)}
              className={cn(
                "flex w-full items-center gap-3 px-4 py-3 text-left",
                state === "current" && "border-information-default border-t-2",
                state === "todo" && "text-muted-foreground",
                // A step that cannot be reached must not read as a control.
                state === "current" || (state === "todo" && !forward)
                  ? "cursor-default"
                  : "hover:bg-muted/40",
              )}
            >
              <span
                className={cn(
                  "flex size-6 shrink-0 items-center justify-center font-mono text-xs",
                  state === "current" && "bg-information-default text-white",
                  state === "done" && "text-foreground",
                  state === "todo" && "border-border border",
                )}
              >
                {state === "done" ? (
                  <Check className="size-3.5" />
                ) : (
                  String(index + 1).padStart(2, "0")
                )}
              </span>
              <span className="min-w-0">
                <span
                  className={cn(
                    "block font-mono text-[10px] tracking-[0.08em] uppercase",
                    state === "current"
                      ? "text-primary"
                      : "text-muted-foreground",
                  )}
                >
                  {state === "done"
                    ? "Done"
                    : state === "current"
                      ? "Current"
                      : `${String(index + 1).padStart(2, "0")}/${String(steps.length).padStart(2, "0")}`}
                </span>
                <span className="block truncate text-sm">{label}</span>
              </span>
            </button>
          </li>
        );
      })}
    </ol>
  );
}

export function WizardSummary({
  step,
  stepCount,
  name,
  rows,
  servers,
}: {
  step: number;
  stepCount: number;
  name: string;
  rows: { label: string; value: ReactNode }[];
  servers: { id: string; name: string; detail: string }[];
}): JSX.Element {
  return (
    <aside className="border-border bg-card sticky top-4 border shadow-sm">
      {/* The brand's spectrum hairline, used once on the page: it marks the
          rail as the thing carrying the decisions without borrowing the blue
          that means "the step you are on". */}
      <div
        aria-hidden="true"
        className="h-0.5 w-full"
        style={{
          background:
            "linear-gradient(90deg,#320F1E 0%,#C83228 13%,#FB873F 25%,#D2DC91 38%,#5A8250 50%,#002314 62%,#00143C 74%,#2873D7 86%,#9BC3FF 100%)",
        }}
      />
      <div className="space-y-4 p-4">
        <div className="flex items-baseline justify-between gap-3">
          <span className="text-muted-foreground font-mono text-[10px] tracking-[0.08em] uppercase">
            Summary
          </span>
          <span className="text-muted-foreground font-mono text-[10px] tracking-[0.08em] uppercase">
            Step {step} of {stepCount}
          </span>
        </div>
        <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
          {/* The name is a decision like the others, so it is labelled like
              the others. Until it is typed it is a bar: a placeholder word
              would read as the agent's name rather than as its absence. */}
          <div className="contents">
            <dt className="text-muted-foreground">Name</dt>
            <dd className="min-w-0 text-right break-words">
              {name || <Skeleton className="ml-auto h-5 w-28" />}
            </dd>
          </div>
          {rows.map((row) => (
            <div key={row.label} className="contents">
              <dt className="text-muted-foreground">{row.label}</dt>
              <dd className="min-w-0 text-right break-words">{row.value}</dd>
            </div>
          ))}
        </dl>
      </div>
      <div className="border-border border-t">
        <div className="flex items-center justify-between px-4 py-2">
          <span className="text-muted-foreground font-mono text-[10px] tracking-[0.08em] uppercase">
            Servers
          </span>
          <span className="text-muted-foreground font-mono text-xs">
            {servers.length}
          </span>
        </div>
        {servers.length === 0 ? (
          <Text muted small className="px-4 pb-3">
            None selected
          </Text>
        ) : (
          <ul className="divide-border border-border divide-y border-t">
            {servers.map((server) => (
              <li
                key={server.id}
                className="flex items-center justify-between gap-3 px-4 py-2 text-sm"
              >
                <span className="min-w-0 truncate">{server.name}</span>
                <span className="text-muted-foreground shrink-0 font-mono text-xs">
                  {server.detail}
                </span>
              </li>
            ))}
          </ul>
        )}
      </div>
    </aside>
  );
}

export function WizardFooter({
  note,
  onBack,
  primary,
}: {
  note: ReactNode;
  onBack?: () => void;
  primary: ReactNode;
}): JSX.Element {
  return (
    <div className="border-border mt-8 flex items-center justify-between gap-6 border-t pt-5">
      <Text muted small>
        {note}
      </Text>
      <div className="flex shrink-0 gap-2">
        {onBack && (
          <Button variant="secondary" onClick={onBack}>
            Back
          </Button>
        )}
        {primary}
      </div>
    </div>
  );
}

/** A step's own heading and lede, above its controls. */
export function WizardStepHeader({
  title,
  description,
}: {
  title: string;
  description: ReactNode;
}): JSX.Element {
  return (
    <div className="space-y-1">
      <h2 className="text-lg font-semibold">{title}</h2>
      <Text muted small>
        {description}
      </Text>
    </div>
  );
}
