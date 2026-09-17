import type { Deployment } from "@gram/client/models/components/deployment.js";
import type { DeploymentSummary } from "@gram/client/models/components/deploymentsummary.js";
import { describe, expect, it } from "vitest";
import {
  attachmentTypeForSourceKind,
  deploymentCountsForSourceKind,
  deploymentStatusBadge,
} from "./sourceVersions";

const deployment = {
  id: "dep_1",
  status: "completed",
  createdAt: new Date("2026-01-01T00:00:00Z"),
  userId: "user_1",
  externalMcpAssetCount: 1,
  externalMcpToolCount: 2,
  functionsAssetCount: 3,
  functionsToolCount: 4,
  openapiv3AssetCount: 5,
  openapiv3ToolCount: 6,
} as DeploymentSummary;

describe("attachmentTypeForSourceKind", () => {
  it("maps each source kind to its log attachment type", () => {
    expect(attachmentTypeForSourceKind("openapi")).toBe("openapi");
    expect(attachmentTypeForSourceKind("function")).toBe("functions");
  });
});

describe("deploymentCountsForSourceKind", () => {
  it("reads only the counts for the source's kind", () => {
    expect(deploymentCountsForSourceKind("openapi", deployment)).toEqual({
      assetLabel: "OpenAPI documents",
      assetCount: 5,
      toolCount: 6,
    });
    expect(deploymentCountsForSourceKind("function", deployment)).toEqual({
      assetLabel: "Functions",
      assetCount: 3,
      toolCount: 4,
    });
  });

  it("counts function assets from the full deployment when it is the same one", () => {
    // The summary counts by tools, so a resource-only function reads as zero.
    const summary = { ...deployment, functionsAssetCount: 0 };
    const full = {
      id: "dep_1",
      functionsAssets: [{ id: "fn_1" }],
    } as Deployment;
    expect(
      deploymentCountsForSourceKind("function", summary, full).assetCount,
    ).toBe(1);
    expect(
      deploymentCountsForSourceKind("function", summary, {
        ...full,
        id: "dep_2",
      }).assetCount,
    ).toBe(0);
  });
});

describe("deploymentStatusBadge", () => {
  it("lets the active deployment outrank its completed status", () => {
    expect(deploymentStatusBadge("completed", true)).toEqual({
      variant: "success",
      label: "Active",
    });
    expect(deploymentStatusBadge("completed", false)).toEqual({
      variant: "neutral",
      label: "Completed",
    });
  });

  it("reads failed and in-flight statuses by tone", () => {
    expect(deploymentStatusBadge("failed", false).variant).toBe("destructive");
    expect(deploymentStatusBadge("pending", false)).toEqual({
      variant: "warning",
      label: "Pending",
    });
    // "created" is the initial status; it has no label of its own.
    expect(deploymentStatusBadge("created", false)).toEqual({
      variant: "warning",
      label: "created",
    });
  });
});
