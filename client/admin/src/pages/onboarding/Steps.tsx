import { useQuery } from "@tanstack/react-query";
import { ChevronRightIcon } from "lucide-react";
import { useRef, useState, type JSX } from "react";
import type { AdminOnboardingStep } from "@gram/admin-client/models/components/adminonboardingstep";

import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useOnUnmount } from "@/hooks/useOnUnmount";
import { errorMessage } from "@/lib/gramAdminApi";
import { onboardingStepsQuery } from "@/lib/gramAdminClient";
import { cn } from "@/lib/utils";

/** The row's DOM id, which the Requires links point at. */
function rowID(slug: string): string {
  return `step-${slug}`;
}

/** Callback ref for the row a link revealed: scroll to it as it attaches. */
function scrollToRow(row: HTMLTableRowElement | null): void {
  // jsdom has no scrollIntoView.
  if (row && typeof row.scrollIntoView === "function") {
    row.scrollIntoView({ block: "center", behavior: "smooth" });
  }
}

/** How long a revealed row stays highlighted. */
const HIGHLIGHT_MS = 1500;

/**
 * The steps the setup wizard can walk, read from the mirror the server
 * writes at start-up. Read-only: a step is a card the dashboard renders, so
 * adding or changing one is a code change and a deploy. A group's cards sit
 * under it and show when the group is expanded; a prerequisite links to its
 * own row, expanding the group that holds it.
 */
export function OnboardingSteps(): JSX.Element {
  const query = useQuery({ ...onboardingStepsQuery(), throwOnError: false });
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(new Set());
  const [highlighted, setHighlighted] = useState<string | null>(null);
  const highlightTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useOnUnmount(() => {
    if (highlightTimer.current) clearTimeout(highlightTimer.current);
  });

  if (query.isPending) return <p role="status">Loading steps…</p>;
  if (!query.data)
    return (
      <div className="space-y-3">
        <h1 className="text-2xl font-semibold">Steps</h1>
        <p role="alert">{errorMessage(query.error)}</p>
        <Button onClick={() => void query.refetch()}>Retry</Button>
      </div>
    );

  const steps = query.data.steps;
  const bySlug = new Map(steps.map((step) => [step.slug, step]));
  const cardCount = new Map<string, number>();
  for (const step of steps) {
    if (step.parentSlug) {
      cardCount.set(step.parentSlug, (cardCount.get(step.parentSlug) ?? 0) + 1);
    }
  }
  const toggle = (slug: string) =>
    setExpanded((current) => {
      const next = new Set(current);
      if (next.has(slug)) next.delete(slug);
      else next.add(slug);
      return next;
    });
  // Show a prerequisite's row: open the group it sits in, then scroll to it
  // (the row's ref does the scrolling) and let the highlight fade.
  const reveal = (slug: string) => {
    const parent = bySlug.get(slug)?.parentSlug;
    if (parent) setExpanded((current) => new Set(current).add(parent));
    setHighlighted(slug);
    if (highlightTimer.current) clearTimeout(highlightTimer.current);
    highlightTimer.current = setTimeout(
      () => setHighlighted(null),
      HIGHLIGHT_MS,
    );
  };
  const visible = steps.filter(
    (step) => !step.parentSlug || expanded.has(step.parentSlug),
  );

  return (
    <div className="space-y-4">
      <div className="space-y-1">
        <h1 className="text-2xl font-semibold">Steps</h1>
        <p className="text-muted-foreground max-w-3xl text-sm">
          Every step the setup wizard can walk, in order. Steps are defined in
          code, so changing one is a deploy.
        </p>
      </div>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Step</TableHead>
            <TableHead>Description</TableHead>
            <TableHead>Requires</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {visible.map((step) => (
            <StepRow
              key={step.slug}
              step={step}
              cards={cardCount.get(step.slug) ?? 0}
              open={expanded.has(step.slug)}
              highlighted={highlighted === step.slug}
              titleOf={(slug) => bySlug.get(slug)?.title ?? slug}
              onToggle={() => toggle(step.slug)}
              onReveal={reveal}
            />
          ))}
        </TableBody>
      </Table>
    </div>
  );
}

function StepRow({
  step,
  cards,
  open,
  highlighted,
  titleOf,
  onToggle,
  onReveal,
}: {
  step: AdminOnboardingStep;
  /** How many cards sit under this step; a group with none is a plain row. */
  cards: number;
  open: boolean;
  highlighted: boolean;
  titleOf: (slug: string) => string;
  onToggle: () => void;
  onReveal: (slug: string) => void;
}): JSX.Element {
  return (
    <TableRow
      ref={highlighted ? scrollToRow : undefined}
      id={rowID(step.slug)}
      data-step={step.slug}
      data-parent={step.parentSlug}
      data-highlighted={highlighted || undefined}
      className={cn(
        "transition-colors",
        step.parentSlug && "bg-muted/40",
        highlighted && "bg-accent",
      )}
    >
      <TableCell
        className={cn(
          "align-top font-medium whitespace-normal",
          step.parentSlug && "pl-10",
        )}
      >
        {cards > 0 ? (
          <button
            type="button"
            aria-expanded={open}
            className="flex items-center gap-1 text-left"
            onClick={onToggle}
          >
            <ChevronRightIcon
              aria-hidden="true"
              className={cn(
                "size-4 shrink-0 transition-transform",
                open && "rotate-90",
              )}
            />
            {step.title}
          </button>
        ) : (
          step.title
        )}
      </TableCell>
      <TableCell className="text-muted-foreground max-w-xl align-top whitespace-normal">
        {step.description}
      </TableCell>
      <TableCell className="align-top whitespace-normal">
        {step.requires.length > 0 ? (
          step.requires.map((slug, index) => (
            <span key={slug}>
              {index > 0 ? ", " : null}
              <a
                href={`#${rowID(slug)}`}
                className="underline underline-offset-4 hover:no-underline"
                onClick={(event) => {
                  event.preventDefault();
                  onReveal(slug);
                }}
              >
                {titleOf(slug)}
              </a>
            </span>
          ))
        ) : (
          <span className="text-muted-foreground">None</span>
        )}
      </TableCell>
    </TableRow>
  );
}
