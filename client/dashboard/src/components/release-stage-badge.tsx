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
  // Preview = experimental, shape may change. Uses the Moonshine `warning`
  // semantic palette so it reads as "use with caution" without screaming
  // destructive — and stays in lockstep with brand if the warning hue is
  // ever retuned at the token level.
  preview: "bg-warning-softest text-default-warning border-warning-default/40",
  // Beta = stable enough for production but still evolving. Uses the
  // Moonshine `information` semantic palette (Speakeasy brand blue) — the
  // same family that backs the `--feature` token, which is what the design
  // system reserves for "new product feature" callouts.
  beta: "bg-information-softest text-default-information border-information-default/40",
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
