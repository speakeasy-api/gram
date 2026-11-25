import { cn } from "@/lib/utils";
import { type ReleaseStage, stageLabel, stageTextClass } from "./release-stage";
import { SimpleTooltip } from "./ui/Tooltip";

export type { ReleaseStage } from "./release-stage";

type ReleaseStageBadgeProps = {
  stage: ReleaseStage;
  /** When true, omit the tooltip wrapper (e.g., inside a parent that already has a tooltip). */
  noTooltip?: boolean;
  className?: string;
};

const stageTooltip: Record<ReleaseStage, string> = {
  preview:
    "Preview features are early and may change. We're sharing them to gather feedback.",
  beta: "Beta features are stable enough for production use but are still evolving.",
};

export function ReleaseStageBadge({
  stage,
  noTooltip = false,
  className,
}: ReleaseStageBadgeProps): JSX.Element {
  const pill = (
    <span
      className={cn(
        "border-border bg-card inline-flex items-center border px-1.5 py-px font-mono text-[10px] tracking-[0.08em] uppercase",
        stageTextClass[stage],
        className,
      )}
      data-release-stage={stage}
    >
      {stageLabel[stage]}
    </span>
  );

  if (noTooltip) return pill;

  return <SimpleTooltip tooltip={stageTooltip[stage]}>{pill}</SimpleTooltip>;
}

// A sidebar row's stage marker: a dot in the stage colour while the row is
// idle, which grows into the stage tag once the row is the current page. A tag
// on every idle row pulled the eye like a selection; the dot only says "this
// one is different" and leaves the active row as the loudest thing.
//
// One element morphs between both shapes. Its text column goes from 0fr to
// 1fr, so the width follows the label without measuring it. The label is
// clipped, not removed, so screen readers still hear the stage on idle rows.
const stageDotClass: Record<ReleaseStage, string> = {
  preview: "bg-[color:var(--text-default-warning)]",
  beta: "bg-[color:var(--text-default-information)]",
};

export function ReleaseStageMarker({
  stage,
  expanded,
  className,
}: {
  stage: ReleaseStage;
  /** Show the full tag instead of the dot: the row is the current page. */
  expanded: boolean;
  className?: string;
}): JSX.Element {
  return (
    <span
      className={cn(
        "inline-grid shrink-0 items-center overflow-hidden border font-mono text-[10px] leading-none tracking-[0.08em] uppercase",
        "transition-[grid-template-columns,min-width,height,padding,border-radius,background-color,border-color] duration-200 ease-out motion-reduce:transition-none",
        expanded
          ? cn(
              "bg-card h-[18px] min-w-0 grid-cols-[1fr] rounded-none px-1.5",
              stageTextClass[stage],
            )
          : cn(
              "h-1.5 min-w-1.5 grid-cols-[0fr] rounded-[3px] border-transparent px-0",
              stageDotClass[stage],
            ),
        className,
      )}
      data-release-stage={stage}
      data-expanded={expanded ? "" : undefined}
    >
      <span
        className={cn(
          "min-w-0 overflow-hidden transition-opacity duration-200 ease-out motion-reduce:transition-none",
          expanded ? "opacity-100" : "opacity-0",
        )}
      >
        {stageLabel[stage]}
      </span>
    </span>
  );
}
