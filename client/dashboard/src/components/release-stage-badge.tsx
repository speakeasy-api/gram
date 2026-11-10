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
