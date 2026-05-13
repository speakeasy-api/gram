import type { ShadowMCPAccessRule } from "@gram/client/models/components/shadowmcpaccessrule.js";
import type { ShadowMCPApprovalRequest } from "@gram/client/models/components/shadowmcpapprovalrequest.js";
import { describe, expect, it } from "vitest";
import {
  getDefaultMatchBreadth,
  getMatchValue,
  getRequestDisplayName,
  roleNamesForIds,
} from "./shadow-mcp-utils";

function approvalRequest(
  overrides: Partial<ShadowMCPApprovalRequest>,
): ShadowMCPApprovalRequest {
  return {
    blockedCount: 1,
    createdAt: new Date("2026-05-01T00:00:00Z"),
    id: "request_1",
    organizationId: "org_1",
    projectId: "project_1",
    requestedAt: new Date("2026-05-01T00:00:00Z"),
    status: "requested",
    updatedAt: new Date("2026-05-01T00:00:00Z"),
    ...overrides,
  };
}

function accessRule(
  overrides: Partial<ShadowMCPAccessRule>,
): ShadowMCPAccessRule {
  return {
    createdAt: new Date("2026-05-01T00:00:00Z"),
    displayName: "Datadog",
    disposition: "allowed",
    id: "rule_1",
    matchBreadth: "full_url",
    matchValue: "https://datadog.example/mcp",
    organizationId: "org_1",
    roleIds: [],
    updatedAt: new Date("2026-05-01T00:00:00Z"),
    ...overrides,
  };
}

describe("shadow-mcp-utils", () => {
  it("defaults to the narrowest available URL match", () => {
    expect(
      getDefaultMatchBreadth(
        approvalRequest({
          observedFullUrl: "https://datadog.example/mcp",
          observedUrlHost: "datadog.example",
          observedServerIdentity: "datadog",
        }),
      ),
    ).toBe("full_url");
  });

  it("falls back to host and server identity when full URL is unavailable", () => {
    expect(
      getDefaultMatchBreadth(
        approvalRequest({
          observedUrlHost: "datadog.example",
          observedServerIdentity: "datadog",
        }),
      ),
    ).toBe("url_host");

    expect(
      getDefaultMatchBreadth(
        approvalRequest({
          observedServerIdentity: "datadog",
        }),
      ),
    ).toBe("server_identity");
  });

  it("returns the selected evidence value for each match breadth", () => {
    const request = approvalRequest({
      observedFullUrl: "https://datadog.example/mcp",
      observedUrlHost: "datadog.example",
      observedServerIdentity: "datadog",
    });

    expect(getMatchValue(request, "full_url")).toBe(
      "https://datadog.example/mcp",
    );
    expect(getMatchValue(request, "url_host")).toBe("datadog.example");
    expect(getMatchValue(request, "server_identity")).toBe("datadog");
  });

  it("falls back when displaying request and rule names", () => {
    expect(
      getRequestDisplayName(
        approvalRequest({
          observedUrlHost: "datadog.example",
        }),
      ),
    ).toBe("datadog.example");

    expect(
      getRequestDisplayName(
        approvalRequest({
          observedFullUrl: "https://datadog.example/mcp",
          observedName: "Datadog",
        }),
      ),
    ).toBe("Datadog");

    expect(
      getMatchValue(
        accessRule({
          observedFullUrl: "https://stripe.example/mcp",
        }),
        "full_url",
      ),
    ).toBe("https://stripe.example/mcp");
  });

  it("maps role ids to names and leaves unknown ids readable", () => {
    expect(
      roleNamesForIds(
        ["role_dev", "role_unknown"],
        [
          {
            id: "role_dev",
            name: "Developers",
          },
        ],
      ),
    ).toEqual(["Developers", "role_unknown"]);
  });
});
