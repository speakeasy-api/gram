import { Badge } from "@speakeasy-api/moonshine";
import { SimpleTooltip } from "./ui/tooltip";

export type ReleaseStage = "preview" | "beta";

type ReleaseStageBadgeProps = {
  stage: ReleaseStage;
  /** Size is kept for API back-compat; both values map to the same Moonshine Badge. */
  size?: "xs" | "sm";
  /** When true, omit the tooltip wrapper (e.g., inside a parent that already has a tooltip). */
  noTooltip?: boolean;
  className?: string;
};

// Map each release stage onto Moonshine's built-in Badge semantic variants.
// `warning` (amber) for preview = "use with caution, may change."
// `information` (Speakeasy brand blue) for beta = "open for use, still evolving."
// Moonshine's Badge handles the shape, padding, typography, and theme/brand
// retuning — we just pick the semantic variant.
const stageVariant: Record<ReleaseStage, "warning" | "information"> = {
  preview: "warning",
  beta: "information",
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
}: ReleaseStageBadgeProps) {
  const pill = (
    <Badge
      variant={stageVariant[stage]}
      background
      className={className}
      data-release-stage={stage}
    >
      <Badge.Text>{stageLabel[stage]}</Badge.Text>
    </Badge>
  );

  if (noTooltip) return pill;

  return <SimpleTooltip tooltip={stageTooltip[stage]}>{pill}</SimpleTooltip>;
}
