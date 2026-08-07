import { cn } from "@/lib/utils";
import { SimpleTooltip } from "./ui/Tooltip";

export type ReleaseStage = "preview" | "beta";

type ReleaseStageBadgeProps = {
  stage: ReleaseStage;
  /** When true, omit the tooltip wrapper (e.g., inside a parent that already has a tooltip). */
  noTooltip?: boolean;
  className?: string;
};

// Editorial tag chip: square, mono uppercase, hairline border on bg-card.
// Beta keeps the brand-blue text (the historical stage color); preview uses
// warning-orange. Color lives in text + border only — no tinted washes.
const stageTextClass: Record<ReleaseStage, string> = {
  preview: "text-default-warning border-warning-softest",
  beta: "text-default-information border-information-softest",
};

const stageLabel: Record<ReleaseStage, string> = {
  preview: "Preview",
  beta: "Beta",
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
