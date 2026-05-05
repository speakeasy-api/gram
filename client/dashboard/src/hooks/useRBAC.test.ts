import { describe, expect, it } from "vitest";
import { resourceKindForScope, selectorMatches } from "./useRBAC";

describe("resourceKindForScope", () => {
  it("returns 'project' for project scopes", () => {
    expect(resourceKindForScope("project:read")).toBe("project");
    expect(resourceKindForScope("project:write")).toBe("project");
  });

  it("returns 'mcp' for mcp scopes", () => {
    expect(resourceKindForScope("mcp:read")).toBe("mcp");
    expect(resourceKindForScope("mcp:write")).toBe("mcp");
    expect(resourceKindForScope("mcp:connect")).toBe("mcp");
  });

  it("returns 'mcp' for remote-mcp scopes", () => {
    expect(resourceKindForScope("remote-mcp:read")).toBe("mcp");
    expect(resourceKindForScope("remote-mcp:write")).toBe("mcp");
    expect(resourceKindForScope("remote-mcp:connect")).toBe("mcp");
  });

  it("returns 'org' for org scopes", () => {
    expect(resourceKindForScope("org:read")).toBe("org");
    expect(resourceKindForScope("org:admin")).toBe("org");
  });

  it("returns 'environment' for environment scopes", () => {
    expect(resourceKindForScope("environment:read")).toBe("environment");
    expect(resourceKindForScope("environment:write")).toBe("environment");
  });

  it("returns '*' for unknown scope families", () => {
    expect(resourceKindForScope("root")).toBe("*");
    expect(resourceKindForScope("unknown:thing")).toBe("*");
  });
});

describe("selectorMatches", () => {
  it("wildcard grant matches anything", () => {
    const grant = { resourceId: "*" };
    expect(selectorMatches(grant, { resourceId: "proj_123" })).toBe(true);
    expect(selectorMatches(grant, { resourceId: "anything" })).toBe(true);
  });

  it("empty grant matches anything", () => {
    expect(selectorMatches({}, { resourceId: "proj_123" })).toBe(true);
    expect(selectorMatches({}, {})).toBe(true);
  });

  it("exact key match", () => {
    const grant = { resourceId: "proj_123" };
    expect(selectorMatches(grant, { resourceId: "proj_123" })).toBe(true);
    expect(selectorMatches(grant, { resourceId: "proj_456" })).toBe(false);
  });

  it("grant key absent from check is skipped", () => {
    const grant = { resourceId: "proj_123" };
    expect(selectorMatches(grant, {})).toBe(true);
    expect(selectorMatches(grant, { otherKey: "val" })).toBe(true);
  });

  it("multiple keys must all match", () => {
    const grant = { resourceId: "proj_123", tool: "tool_abc" };
    expect(
      selectorMatches(grant, { resourceId: "proj_123", tool: "tool_abc" }),
    ).toBe(true);
    expect(
      selectorMatches(grant, { resourceId: "proj_123", tool: "tool_xyz" }),
    ).toBe(false);
    // check without tool — not constraining that dimension
    expect(selectorMatches(grant, { resourceId: "proj_123" })).toBe(true);
  });

  it("resourceKind mismatch fails", () => {
    const grant = { resourceKind: "project", resourceId: "proj_123" };
    expect(
      selectorMatches(grant, {
        resourceKind: "mcp",
        resourceId: "proj_123",
      }),
    ).toBe(false);
  });

  it("resourceKind wildcard matches any kind", () => {
    const grant = { resourceKind: "*", resourceId: "*" };
    expect(
      selectorMatches(grant, {
        resourceKind: "project",
        resourceId: "proj_123",
      }),
    ).toBe(true);
    expect(
      selectorMatches(grant, {
        resourceKind: "mcp",
        resourceId: "tool_a",
      }),
    ).toBe(true);
  });

  it("disposition grant matches connection check without disposition", () => {
    const grant = {
      resourceKind: "mcp",
      resourceId: "*",
      disposition: "read_only",
    };
    const check = { resourceKind: "mcp", resourceId: "toolsetA" };
    expect(selectorMatches(grant, check)).toBe(true);
  });

  it("disposition grant denies wrong disposition", () => {
    const grant = {
      resourceKind: "mcp",
      resourceId: "*",
      disposition: "read_only",
    };
    expect(
      selectorMatches(grant, {
        resourceKind: "mcp",
        resourceId: "toolsetA",
        disposition: "read_only",
      }),
    ).toBe(true);
    expect(
      selectorMatches(grant, {
        resourceKind: "mcp",
        resourceId: "toolsetA",
        disposition: "destructive",
      }),
    ).toBe(false);
  });
});
