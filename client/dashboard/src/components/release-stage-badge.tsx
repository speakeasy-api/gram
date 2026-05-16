import { cn } from "@/lib/utils";
import { SimpleTooltip } from "./ui/tooltip";

export type ReleaseStage = "preview" | "beta";

type ReleaseStageBadgeProps = {
  stage: ReleaseStage;
  size?: "xs" | "sm";
  /** When true, omit the tooltip wrapper (e.g., inside a sidebar item that already has a tooltip). */
  noTooltip?: boolean;
  className?: string;
};

// Styling matches `ProductTierBadge` (the Enterprise pill on the Billing nav
// entry) so the two badge families read as one component family in the nav
// rail. Same shape (rounded-sm, px-1, py-0.5, text-xs), no border, title-case
// label. Only the bg/text tokens differ — warning palette for preview,
// information palette for beta.
const stageStyles: Record<ReleaseStage, string> = {
  preview: "bg-warning-softest text-default-warning",
  beta: "bg-information-softest text-default-information",
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
  size = "sm",
  noTooltip = false,
  className,
}: ReleaseStageBadgeProps) {
  const sizeClass =
    size === "xs" ? "text-xs px-1 py-0.5" : "text-xs px-1.5 py-0.5";

  const pill = (
    <span
      className={cn(
        "inline-flex w-fit shrink-0 items-center rounded-sm",
        sizeClass,
        stageStyles[stage],
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
