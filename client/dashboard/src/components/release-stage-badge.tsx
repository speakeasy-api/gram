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

const stageStyles: Record<ReleaseStage, string> = {
  // Preview = experimental, shape may change. Uses the warning palette so it
  // reads as "use with caution" without screaming destructive.
  preview: "bg-warning-softest text-warning-default border-warning-default/30",
  // Beta = feature is open for use but still pre-GA. Uses the violet brand
  // tone shared with the paid product tier badge, so beta features feel like
  // promoted/active surfaces rather than dangerous ones.
  beta: "bg-violet-500/10 text-violet-700 border-violet-500/30 dark:text-violet-300",
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
    size === "xs"
      ? "text-[10px] leading-none px-1 py-0.5"
      : "text-xs px-1.5 py-0.5";

  const pill = (
    <span
      className={cn(
        "inline-flex w-fit shrink-0 items-center rounded-sm border font-medium tracking-wide uppercase",
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
