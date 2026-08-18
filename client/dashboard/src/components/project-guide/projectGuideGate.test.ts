import { describe, expect, it } from "vitest";
import {
  decideProjectGuideStatus,
  type ProjectGuideGateInput,
} from "./projectGuideGate";

/** An admin on an empty project with logs on: the one case that shows the guide. */
function emptyAdminProject(
  overrides: Partial<ProjectGuideGateInput> = {},
): ProjectGuideGateInput {
  return {
    hasProjectSlug: true,
    rbacLoading: false,
    isAdmin: true,
    featuresPending: false,
    logsEnabled: true,
    dismissed: false,
    started: false,
    serversPending: false,
    serversError: false,
    hasServers: false,
    policiesPending: false,
    policiesError: false,
    hasPolicies: false,
    overviewPending: false,
    overviewError: false,
    hasData: false,
    ...overrides,
  };
}

describe("decideProjectGuideStatus", () => {
  it("shows the guide on an empty project for an admin with logs", () => {
    expect(decideProjectGuideStatus(emptyAdminProject())).toBe("guide");
  });

  it("shows the dashboard to a non-admin without running any check", () => {
    expect(
      decideProjectGuideStatus(
        emptyAdminProject({ isAdmin: false, serversPending: true }),
      ),
    ).toBe("dashboard");
  });

  it("shows the dashboard when logs are disabled", () => {
    expect(
      decideProjectGuideStatus(emptyAdminProject({ logsEnabled: false })),
    ).toBe("dashboard");
  });

  it("waits while RBAC grants are loading", () => {
    expect(
      decideProjectGuideStatus(emptyAdminProject({ rbacLoading: true })),
    ).toBe("pending");
  });

  it("waits while product features are loading", () => {
    expect(
      decideProjectGuideStatus(emptyAdminProject({ featuresPending: true })),
    ).toBe("pending");
  });

  it("shows the dashboard once dismissed", () => {
    expect(
      decideProjectGuideStatus(emptyAdminProject({ dismissed: true })),
    ).toBe("dashboard");
  });

  it("keeps the guide for a started run even after it created a server and a policy", () => {
    expect(
      decideProjectGuideStatus(
        emptyAdminProject({
          started: true,
          hasServers: true,
          hasPolicies: true,
          hasData: true,
        }),
      ),
    ).toBe("guide");
  });

  it("skips the emptiness queries entirely for a started run", () => {
    expect(
      decideProjectGuideStatus(
        emptyAdminProject({
          started: true,
          serversPending: true,
          policiesPending: true,
          overviewPending: true,
        }),
      ),
    ).toBe("guide");
  });

  it("shows the dashboard when the project already has an MCP server", () => {
    expect(
      decideProjectGuideStatus(emptyAdminProject({ hasServers: true })),
    ).toBe("dashboard");
  });

  it("shows the dashboard when the project already has a risk policy", () => {
    expect(
      decideProjectGuideStatus(emptyAdminProject({ hasPolicies: true })),
    ).toBe("dashboard");
  });

  it("shows the dashboard when the servers check could not be read", () => {
    expect(
      decideProjectGuideStatus(emptyAdminProject({ serversError: true })),
    ).toBe("dashboard");
  });

  it("shows the dashboard when the policies check could not be read", () => {
    expect(
      decideProjectGuideStatus(emptyAdminProject({ policiesError: true })),
    ).toBe("dashboard");
  });

  it("shows the dashboard when telemetry arrived without a server or policy", () => {
    expect(decideProjectGuideStatus(emptyAdminProject({ hasData: true }))).toBe(
      "dashboard",
    );
  });

  it("waits for the cheap checks before deciding", () => {
    expect(
      decideProjectGuideStatus(emptyAdminProject({ serversPending: true })),
    ).toBe("pending");
    expect(
      decideProjectGuideStatus(emptyAdminProject({ policiesPending: true })),
    ).toBe("pending");
  });

  it("waits for the overview only once both cheap checks came back empty", () => {
    expect(
      decideProjectGuideStatus(emptyAdminProject({ overviewPending: true })),
    ).toBe("pending");
    expect(
      decideProjectGuideStatus(
        emptyAdminProject({ hasServers: true, overviewPending: true }),
      ),
    ).toBe("dashboard");
  });

  it("shows the dashboard when the overview could not be read", () => {
    expect(
      decideProjectGuideStatus(emptyAdminProject({ overviewError: true })),
    ).toBe("dashboard");
  });

  it("shows the dashboard when no project is selected", () => {
    expect(
      decideProjectGuideStatus(emptyAdminProject({ hasProjectSlug: false })),
    ).toBe("dashboard");
  });

  it("waits on RBAC even when the not-yet-loaded grants read as non-admin", () => {
    expect(
      decideProjectGuideStatus(
        emptyAdminProject({ rbacLoading: true, isAdmin: false }),
      ),
    ).toBe("pending");
  });

  it("shows the dashboard to a non-admin even with a stale started flag", () => {
    expect(
      decideProjectGuideStatus(
        emptyAdminProject({ started: true, isAdmin: false }),
      ),
    ).toBe("dashboard");
  });

  it("waits for the overview even though an unread overview reads as hasData", () => {
    expect(
      decideProjectGuideStatus(
        emptyAdminProject({ overviewPending: true, hasData: true }),
      ),
    ).toBe("pending");
  });

  it("shows the dashboard for an org-level route even while RBAC is loading", () => {
    expect(
      decideProjectGuideStatus(
        emptyAdminProject({ hasProjectSlug: false, rbacLoading: true }),
      ),
    ).toBe("dashboard");
  });
});
