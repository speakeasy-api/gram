import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import type { WorkloadDeviceJoinTokenSummary } from "@gram/client/models/components/workloaddevicejointokensummary.js";
import { describe, expect, it } from "vitest";
import {
  acceptsHooks,
  allowsDeviceSync,
  deviceEnrollmentPolicyGrants,
  managedConfig,
  redeemedToken,
  watchedTokensFinished,
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
    expect(allowsDeviceSync(policy, "org_two")).toBe(false);
    expect(acceptsHooks(policy, "org_two", "project_one")).toBe(false);
  });

  it("recognizes wildcard organization and project grants", () => {
    expect(
      allowsDeviceSync(
        [
          grant("org:device_agent_sync", {
            resourceKind: "org",
            resourceId: "*",
          }),
        ],
        "org_one",
      ),
    ).toBe(true);
    expect(
      acceptsHooks(
        [
          grant("org:hooks_ingest", {
            resourceKind: "org",
            resourceId: "*",
            projectId: "*",
          }),
        ],
        "org_one",
        "project_two",
      ),
    ).toBe(true);
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

  it.each([
    "http://example.test",
    "http://localhost:8080",
    "invalid",
    "https://user:password@example.test",
  ])("refuses unsafe control plane %s", (controlPlaneURL) => {
    expect(() =>
      managedConfig({
        joinToken: "example",
        hooksProjectSlug: "default",
        controlPlaneURL,
      }),
    ).toThrow(/HTTPS/);
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

  it("does not confuse two tokens with the same expiry", () => {
    expect(
      redeemedToken(
        [token({ id: "other_token", status: "redeemed" })],
        [{ id: "token_one" }],
      ),
    ).toBeUndefined();
  });

  it("ignores a watched token that is still active", () => {
    expect(redeemedToken([token({})], [{ id: "token_one" }])).toBeUndefined();
  });
});

describe("watchedTokensFinished", () => {
  it.each(["expired", "revoked"] as const)("stops on %s tokens", (status) => {
    expect(
      watchedTokensFinished([token({ status })], [{ id: "token_one" }]),
    ).toBe(true);
  });
  it("stops at the issued expiry even if the token is not in the response", () => {
    expect(
      watchedTokensFinished(
        [],
        [{ id: "token_one", expiresAt: new Date(1000) }],
        1000,
      ),
    ).toBe(true);
    expect(
      watchedTokensFinished(
        [],
        [{ id: "token_one", expiresAt: new Date(1000) }],
        999,
      ),
    ).toBe(false);
  });
  it("keeps polling while any watched token is active", () => {
    expect(
      watchedTokensFinished(
        [
          token({ status: "expired" }),
          token({ id: "token_two", expiresAt: new Date(2000) }),
        ],
        [{ id: "token_one" }, { id: "token_two" }],
        1000,
      ),
    ).toBe(false);
  });
});
