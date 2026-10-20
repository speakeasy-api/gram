import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import type { WorkloadDeviceJoinTokenSummary } from "@gram/client/models/components/workloaddevicejointokensummary.js";
import { describe, expect, it } from "vitest";
import {
  acceptsHooks,
  allowsDeviceSync,
  deviceEnrollmentPolicyGrants,
  managedConfig,
  redeemedToken,
} from "./device-enrollment";

function grant(
  scope: string,
  selector: AgentPolicyGrant["selector"],
): AgentPolicyGrant {
  return {
    id: scope,
    effect: "allow",
    scope,
    selector,
    createdAt: new Date(),
    updatedAt: new Date(),
  };
}

function token(
  overrides: Partial<WorkloadDeviceJoinTokenSummary>,
): WorkloadDeviceJoinTokenSummary {
  return {
    id: "token_one",
    agentId: "agent_one",
    createdAt: new Date("2026-10-20T10:00:00Z"),
    createdByUserId: "user_one",
    expiresAt: new Date("2026-10-20T11:00:00Z"),
    status: "active",
    ...overrides,
  };
}

describe("deviceEnrollmentPolicyGrants", () => {
  it("always grants device sync on the organization", () => {
    expect(deviceEnrollmentPolicyGrants({ organizationId: "org_one" })).toEqual(
      [
        {
          effect: "allow",
          scope: "org:device_agent_sync",
          selector: { resourceKind: "org", resourceId: "org_one" },
        },
      ],
    );
  });

  it("adds hooks for the chosen project only", () => {
    expect(
      deviceEnrollmentPolicyGrants({
        organizationId: "org_one",
        hooksProjectId: "project_one",
      })[1],
    ).toEqual({
      effect: "allow",
      scope: "org:hooks_ingest",
      selector: {
        resourceKind: "org",
        resourceId: "org_one",
        projectId: "project_one",
      },
    });
  });
});

describe("reading an agent's policy", () => {
  it("finds device sync and per-project hooks", () => {
    const policy = [
      grant("org:device_agent_sync", {
        resourceKind: "org",
        resourceId: "org_one",
      }),
      grant("org:hooks_ingest", {
        resourceKind: "org",
        resourceId: "org_one",
        projectId: "project_one",
      }),
    ];
    expect(allowsDeviceSync(policy, "org_one")).toBe(true);
    expect(acceptsHooks(policy, "org_one", "project_one")).toBe(true);
    expect(acceptsHooks(policy, "org_one", "project_two")).toBe(false);
    expect(allowsDeviceSync([], "org_one")).toBe(false);
  });

  it("reads an organization-wide hooks grant as covering every project", () => {
    const policy = [
      grant("org:hooks_ingest", { resourceKind: "org", resourceId: "org_one" }),
    ];
    expect(acceptsHooks(policy, "org_one", "project_two")).toBe(true);
  });
});

describe("managedConfig", () => {
  it("puts the token at the top level and leaves the default control plane out", () => {
    const parsed = JSON.parse(
      managedConfig({
        joinToken: "gram_dj_example",
        hooksProjectSlug: "default",
        controlPlaneURL: "https://app.getgram.ai/",
      }),
    );
    expect(parsed).toEqual({
      v: 1,
      workload_identity: {
        source: "device_key",
        hooks_project_slug: "default",
      },
      join_token: "gram_dj_example",
    });
  });

  it("names any other control plane", () => {
    const parsed = JSON.parse(
      managedConfig({
        joinToken: "gram_dj_example",
        hooksProjectSlug: "default",
        controlPlaneURL: "https://localhost:8080/rpc",
      }),
    );
    expect(parsed._control_plane_url).toBe("https://localhost:8080");
  });
});

describe("redeemedToken", () => {
  it("matches a watched id", () => {
    const tokens = [
      token({ status: "redeemed", consumedBySubject: "device:a" }),
    ];
    expect(
      redeemedToken(tokens, [{ id: "token_one" }])?.consumedBySubject,
    ).toBe("device:a");
    expect(redeemedToken(tokens, [{ id: "token_two" }])).toBeUndefined();
  });

  it("matches a manually created token by its expiry, to the second", () => {
    const tokens = [token({ status: "redeemed" })];
    expect(
      redeemedToken(tokens, [
        { expiresAt: new Date("2026-10-20T11:00:00.400Z") },
      ]),
    ).toBeDefined();
  });

  it("ignores a watched token that is still active", () => {
    expect(redeemedToken([token({})], [{ id: "token_one" }])).toBeUndefined();
  });
});
