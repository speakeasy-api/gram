import { Badge } from "@/components/ui/Badge";
import type { BadgeSize } from "@/components/ui/lib/types";
import {
  CAPABILITY_META,
  toolCapability,
  type ToolCapabilityAnnotations,
} from "./toolCapability";

/**
 * Renders a compact read/write/destructive chip for a tool. Returns null when
 * the tool's annotations don't determine a capability.
 */
export function ToolCapabilityBadge({
  annotations,
  size = "sm",
  className,
}: {
  annotations?: ToolCapabilityAnnotations | null;
  size?: BadgeSize;
  className?: string;
}): JSX.Element | null {
  const capability = toolCapability(annotations);
  if (!capability) return null;

  const meta = CAPABILITY_META[capability];
  return (
    <Badge
      variant={meta.variant}
      size={size}
      tooltip={meta.tooltip}
      className={className}
    >
      {meta.label}
    </Badge>
  );
}
