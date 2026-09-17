import type { BadgeVariant } from "@/components/ui/lib/types";
import type { Deployment } from "@gram/client/models/components/deployment.js";
import type { DeploymentSummary } from "@gram/client/models/components/deploymentsummary.js";

export type SourceKind = "openapi" | "function";

// Map dashboard source kinds to backend deployment_logs.attachment_type
// values. See server/internal/deployments/events/log.go.
export function attachmentTypeForSourceKind(
  sourceKind: SourceKind,
): "openapi" | "functions" {
  switch (sourceKind) {
    case "function":
      return "functions";
    case "openapi":
      return "openapi";
  }
}

export interface DeploymentKindCounts {
  assetLabel: string;
  assetCount: number;
  toolCount: number;
}

// A deployment carries every source in the project, but on a source's own
// page only the counts for its kind say anything about it. The summary counts
// function assets by their tools, so a function that only declares resources
// reads as zero there; the full deployment, when it is the one in hand, lists
// the assets themselves.
export function deploymentCountsForSourceKind(
  sourceKind: SourceKind,
  deployment: DeploymentSummary,
  full?: Deployment,
): DeploymentKindCounts {
  const assets = full?.id === deployment.id ? full : undefined;
  switch (sourceKind) {
    case "function":
      return {
        assetLabel: "Functions",
        assetCount:
          assets?.functionsAssets?.length ?? deployment.functionsAssetCount,
        toolCount: deployment.functionsToolCount,
      };
    case "openapi":
      return {
        assetLabel: "OpenAPI documents",
        assetCount: deployment.openapiv3AssetCount,
        toolCount: deployment.openapiv3ToolCount,
      };
  }
}

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
    case "building":
      return { variant: "warning", label: "Building" };
    default:
      return { variant: "warning", label: status };
  }
}
