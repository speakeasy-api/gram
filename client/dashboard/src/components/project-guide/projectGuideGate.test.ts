import { describe, expect, it } from "vitest";
import {
  decideProjectGuideStatus,
  type ProjectGuideGateInput,
} from "./projectGuideGate";

const emptyProject: ProjectGuideGateInput = {
  hasProjectSlug: true,
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
};

describe("decideProjectGuideStatus", () => {
  it.each([
    ["an empty project", {}, "guide"],
    ["a project with an MCP server", { hasServers: true }, "dashboard"],
    ["a project with a risk policy", { hasPolicies: true }, "dashboard"],
    ["a project with telemetry", { hasData: true }, "dashboard"],
    ["an unreadable query", { serversError: true }, "dashboard"],
  ])("shows the expected surface for %s", (_name, overrides, expected) => {
    expect(decideProjectGuideStatus({ ...emptyProject, ...overrides })).toBe(
      expected,
    );
  });

  it("keeps an active guide mounted after it creates project data", () => {
    expect(
      decideProjectGuideStatus({
        ...emptyProject,
        started: true,
        hasServers: true,
        hasPolicies: true,
        hasData: true,
      }),
    ).toBe("guide");
  });

  it("waits for the checks before deciding", () => {
    expect(
      decideProjectGuideStatus({ ...emptyProject, overviewPending: true }),
    ).toBe("pending");
  });
});
