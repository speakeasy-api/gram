import type { BadgeVariant } from "@/components/ui/lib/types";

export type SourceKind = "openapi" | "function";

export interface DeploymentStatusBadge {
  variant: BadgeVariant;
  label: string;
}

// The active deployment is the completed one the dashboard reads from, so it
// outranks a plain "completed" label; everything else reads by its status.
export function deploymentStatusBadge(
  status: string,
  isActive: boolean,
): DeploymentStatusBadge {
  if (isActive) return { variant: "success", label: "Active" };
  switch (status) {
    case "completed":
      return { variant: "neutral", label: "Completed" };
    case "failed":
      return { variant: "destructive", label: "Failed" };
    case "pending":
      return { variant: "warning", label: "Pending" };
    default:
      return { variant: "warning", label: status };
  }
}
