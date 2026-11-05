import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import { SimpleTooltip, TooltipProvider } from "@/components/ui/Tooltip";
import { Check } from "lucide-react";
import { useEffect, useRef, type JSX, type ReactNode } from "react";

/**
 * The frame every provisioning step shares: the five-cell stepper, the body,
 * the summary rail, and one footer that carries the note and the moves.
 *
 * The steps own their content and nothing else — a step that drew its own
 * heading, rail or buttons is what made the old wizard read as five unrelated
 * screens.
 */

type WizardStepState = "done" | "current" | "todo";

export function WizardStepper({
  steps,
  current,
  settledFrom,
  forward = false,
  onJump,
}: {
  steps: { label: string; short: string }[];
  current: number;
  /**
   * Steps from here on are already settled, however far along the reader
   * is: editing an agent that already holds a credential has nothing left
   * to do in the steps that set one up.
   */
  settledFrom?: number;
  /** Whether a step ahead of the current one can be jumped to. */
  forward?: boolean;
  onJump: (index: number) => void;
}): JSX.Element {
  // Five labelled cells is as much as the row carries: a platform's own setup
  // can push it to nine. Past that the steps behind collapse to their tick —
  // they are done, their name is not what the reader needs. Past seven even
  // the names ahead stop fitting, and four columns of wrapped fragments say
  // less than four numbers do, so the steps ahead collapse too and the row
  // becomes: what is finished, what you are on, how much is left.
  const dense = steps.length > 5;
  const crowded = steps.length > 7;

  // Where the row scrolls, the step just reached can sit past its right edge:
  // moving on is a click on a footer button, so nothing in the row is focused
  // and the browser never brings it back by itself.
  const currentCell = useRef<HTMLLIElement | null>(null);
  useEffect(() => {
    // Not smooth, and "nearest" on both axes: the reader did not ask the page
    // to move, so a cell already in view stays where it is and the page itself
    // is never scrolled — only the row that overflows.
    currentCell.current?.scrollIntoView({
      behavior: "instant",
      block: "nearest",
      inline: "nearest",
    });
  }, [current]);

  return (
    // Narrow enough and the cells have nowhere left to go, so the row scrolls
    // rather than squeezing every name into a clipped column.
    <ol
      aria-label="Provisioning steps"
      className="border-border flex overflow-x-auto border"
    >
      {steps.map(({ label, short }, index) => {
        // The step the reader is on is tested first: a settled step they are
        // standing on is still where they are, and marking it done would
        // leave the row showing no position at all.
        const state: WizardStepState =
          index === current
            ? "current"
            : index < current ||
                (settledFrom !== undefined && index >= settledFrom)
              ? "done"
              : "todo";
        // A step behind gives up its name once the row is full: it is done,
        // and its name is not what the reader needs. A step ahead keeps a
        // name, shortened, because a column of bare numbers says nothing
        // about what is still to come.
        const collapsed = dense && state === "done";
        const shortened = crowded && state === "todo";
        // The slack goes to the steps ahead, which still have to be read;
        // the current one is as wide as its own label.
        const cell = (
          <button
            type="button"
            aria-current={state === "current" ? "step" : undefined}
            // Collapsed to a tick, so the step is named here rather than
            // only in the label it no longer shows.
            aria-label={
              collapsed
                ? `${label}${state === "done" ? " (done)" : ""}`
                : undefined
            }
            // A step ahead is a destination only where nothing later is
            // built from a choice not yet made.
            disabled={state === "current" || (state === "todo" && !forward)}
            onClick={() => onJump(index)}
            className={cn(
              "flex h-full min-h-16 w-full items-start gap-3 px-4 py-2.5 text-left",
              collapsed && "justify-center",
              state === "current" && "border-information-default border-t-2",
              // Shaded, so a glance shows how much of the flow is still
              // ahead rather than one flat strip of cells.
              state === "todo" && "text-muted-foreground bg-muted/40",
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
            {!collapsed && (
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
                <span className="min-h-10 text-sm whitespace-nowrap">
                  {shortened ? short : label}
                </span>
              </span>
            )}
          </button>
        );
        return (
          <li
            key={label}
            ref={state === "current" ? currentCell : undefined}
            className={cn(
              "border-border flex border-r last:border-r-0",
              collapsed || shortened
                ? "shrink-0"
                : // Below md the cell keeps a readable width and the row
                  // scrolls; from md up it shares the row as before.
                  "min-w-max flex-1",
            )}
          >
            {/* A tick alone does not say which step it was, and the native
                title waits a second before saying so. */}
            {collapsed ? (
              // Its own provider: the stepper is rendered on its own in
              // tests, and a tooltip without one throws rather than degrades.
              <TooltipProvider>
                <SimpleTooltip tooltip={label}>{cell}</SimpleTooltip>
              </TooltipProvider>
            ) : (
              cell
            )}
          </li>
        );
      })}
    </ol>
  );
}

/** The last thing the reader changed, marked so a glance finds it. */
function Marked({
  on,
  children,
}: {
  on: boolean;
  children: ReactNode;
}): JSX.Element {
  if (!on) return <>{children}</>;
  return (
    <mark className="bg-yellow-200 px-1.5 py-0.5 text-black dark:bg-yellow-300">
      {children}
    </mark>
  );
}

export function WizardSummary({
  step,
  stepCount,
  name,
  rows,
  servers,
  highlight,
}: {
  step: number;
  stepCount: number;
  name: string;
  rows: { label: string; value: ReactNode }[];
  servers: { id: string; name: string; detail: string }[];
  /**
   * What the reader just decided: a row's label, or `server:<id>`.
   *
   * The rail accumulates every choice, so after the third one it looks the
   * same before and after an action. Marking the last change is how a glance
   * confirms the click landed, and on what.
   */
  highlight: string | null;
}): JSX.Element {
  // Cleared past the page header, which is itself sticky and 98px tall: at a
  // smaller offset the rail slides under it and reads as cut off.
  return (
    <aside className="border-border bg-card sticky top-28 border shadow-sm">
      {/* The brand's spectrum hairline, used once on the page: it marks the
          rail as the thing carrying the decisions without borrowing the blue
          that means "the step you are on". */}
      <div
        aria-hidden="true"
        className="h-0.5 w-full"
        style={{
          background: "var(--gradient-brand-primary)",
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
        {/* Label and value share a line while both fit, and the value drops
            to its own full-width line when it does not. A two-column grid
            instead gave the value whatever the label left over, which was
            narrow enough to shatter `mcp:connect` and an organization id
            across several lines. */}
        <dl className="space-y-2 text-sm">
          {/* The name is a decision like the others, so it is labelled like
              the others. Until it is typed it is a bar: a placeholder word
              would read as the agent's name rather than as its absence. */}
          <div className="flex flex-wrap items-baseline justify-between gap-x-4">
            <dt className="text-muted-foreground">Name</dt>
            <dd className="min-w-0 grow basis-28 text-right break-words">
              {name.trim() ? (
                <Marked on={highlight === "Name"}>{name.trim()}</Marked>
              ) : (
                <Skeleton className="ml-auto h-5 w-28" />
              )}
            </dd>
          </div>
          {rows.map((row) => (
            <div
              key={row.label}
              className="flex flex-wrap items-baseline justify-between gap-x-4"
            >
              <dt className="text-muted-foreground">{row.label}</dt>
              <dd className="min-w-0 grow basis-28 text-right break-words">
                <Marked on={highlight === row.label}>{row.value}</Marked>
              </dd>
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
                className="flex min-w-0 flex-col gap-1 px-4 py-2 text-sm"
              >
                <span className="min-w-0 truncate">
                  <Marked on={highlight === `server:${server.id}`}>
                    {server.name}
                  </Marked>
                </span>
                <span className="text-muted-foreground min-w-0 font-mono text-xs break-words">
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
    // Back sits on the left, apart from the move forward: they go opposite
    // ways, and side by side the wrong one is easy to hit.
    <div className="border-border mt-8 flex items-center gap-6 border-t pt-5">
      {onBack && (
        <Button variant="secondary" onClick={onBack}>
          Back
        </Button>
      )}
      <Text muted small className="min-w-0 flex-1">
        {note}
      </Text>
      <div className="flex shrink-0 gap-2">{primary}</div>
    </div>
  );
}

/** A step's own heading and lede, above its controls. */
export function WizardStepHeader({
  title,
  description,
}: {
  title: string;
  /** Left out where the step's own blocks say what it is for. */
  description?: ReactNode;
}): JSX.Element {
  return (
    <div className="space-y-1">
      <h2 className="text-lg font-semibold">{title}</h2>
      {description !== undefined && (
        <Text muted small>
          {description}
        </Text>
      )}
    </div>
  );
}
