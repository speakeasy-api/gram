import { describe, expect, it } from "vitest";
import { getActionCategory, getActionColorConfig } from "./audit-log-colors";

describe("getActionCategory", () => {
  it("categorizes create actions", () => {
    expect(getActionCategory("mcp:create")).toBe("create");
    expect(getActionCategory("asset:create")).toBe("create");
    expect(getActionCategory("project:create")).toBe("create");
  });

  it("categorizes upload as create", () => {
    expect(getActionCategory("asset:upload")).toBe("create");
  });

  it("categorizes update actions", () => {
    expect(getActionCategory("mcp:update")).toBe("update");
    expect(getActionCategory("project:update")).toBe("update");
    expect(getActionCategory("toolset:attach_external_oauth")).toBe("update");
    expect(getActionCategory("toolset:attach_oauth_proxy")).toBe("update");
    expect(getActionCategory("mcp_metadata:update")).toBe("update");
  });

  it("categorizes deploy actions", () => {
    expect(getActionCategory("deployments:redeploy")).toBe("deploy");
    expect(getActionCategory("deployments:evolve")).toBe("deploy");
    expect(getActionCategory("deployments:create")).toBe("deploy");
  });

  it("categorizes destructive actions", () => {
    expect(getActionCategory("project:delete")).toBe("destructive");
    expect(getActionCategory("toolset:delete")).toBe("destructive");
    expect(getActionCategory("toolset:detach_oauth_proxy")).toBe("destructive");
    expect(getActionCategory("toolset:detach_external_oauth")).toBe(
      "destructive",
    );
    expect(getActionCategory("api_key:revoke")).toBe("destructive");
  });

  it("defaults unknown actions to update", () => {
    expect(getActionCategory("unknown:something")).toBe("update");
  });
});

describe("getActionColorConfig", () => {
  it("returns correct colors for each category", () => {
    const create = getActionColorConfig("create");
    expect(create.dot).toBe("bg-emerald-500");
    expect(create.text).toBe("text-emerald-700");
    expect(create.bg).toBe("bg-emerald-50");

    const destructive = getActionColorConfig("destructive");
    expect(destructive.dot).toBe("bg-red-500");
    expect(destructive.text).toBe("text-red-700");
    expect(destructive.bg).toBe("bg-red-50");
  });
});
