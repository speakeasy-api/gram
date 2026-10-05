import { Button } from "@/components/ui/Button";
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
  onJump,
}: {
  steps: string[];
  current: number;
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
              // Only a completed step is a destination: jumping forward would
              // skip the choices the later steps are built from.
              disabled={state !== "done"}
              onClick={() => onJump(index)}
              className={cn(
                "flex w-full items-center gap-3 px-4 py-3 text-left",
                state === "current" && "border-primary border-t-2",
                state === "todo" && "text-muted-foreground",
              )}
            >
              <span
                className={cn(
                  "flex size-6 shrink-0 items-center justify-center font-mono text-xs",
                  state === "current" && "bg-primary text-primary-foreground",
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
    <aside className="border-border border-t-primary sticky top-4 border border-t-2">
      <div className="space-y-4 p-4">
        <div className="flex items-baseline justify-between gap-3">
          <span className="text-muted-foreground font-mono text-[10px] tracking-[0.08em] uppercase">
            Summary
          </span>
          <span className="text-muted-foreground font-mono text-[10px] tracking-[0.08em] uppercase">
            Step {step} of {stepCount}
          </span>
        </div>
        <Text className="font-serif text-2xl">{name || "Unnamed agent"}</Text>
        <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
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
