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

  it("returns '*' for unknown scope families", () => {
    expect(resourceKindForScope("root")).toBe("*");
    expect(resourceKindForScope("unknown:thing")).toBe("*");
  });
});

describe("selectorMatches", () => {
  it("wildcard grant matches anything", () => {
    const grant = { resource_id: "*" };
    expect(selectorMatches(grant, { resource_id: "proj_123" })).toBe(true);
    expect(selectorMatches(grant, { resource_id: "anything" })).toBe(true);
  });

  it("empty grant matches anything", () => {
    expect(selectorMatches({}, { resource_id: "proj_123" })).toBe(true);
    expect(selectorMatches({}, {})).toBe(true);
  });

  it("exact key match", () => {
    const grant = { resource_id: "proj_123" };
    expect(selectorMatches(grant, { resource_id: "proj_123" })).toBe(true);
    expect(selectorMatches(grant, { resource_id: "proj_456" })).toBe(false);
  });

  it("grant key absent from check is skipped", () => {
    const grant = { resource_id: "proj_123" };
    expect(selectorMatches(grant, {})).toBe(true);
    expect(selectorMatches(grant, { other_key: "val" })).toBe(true);
  });

  it("multiple keys must all match", () => {
    const grant = { resource_id: "proj_123", tool_id: "tool_abc" };
    expect(
      selectorMatches(grant, { resource_id: "proj_123", tool_id: "tool_abc" }),
    ).toBe(true);
    expect(
      selectorMatches(grant, { resource_id: "proj_123", tool_id: "tool_xyz" }),
    ).toBe(false);
    // check without tool_id — not constraining that dimension
    expect(selectorMatches(grant, { resource_id: "proj_123" })).toBe(true);
  });

  it("resource_kind mismatch fails", () => {
    const grant = { resource_kind: "project", resource_id: "proj_123" };
    expect(
      selectorMatches(grant, {
        resource_kind: "mcp",
        resource_id: "proj_123",
      }),
    ).toBe(false);
  });

  it("resource_kind wildcard matches any kind", () => {
    const grant = { resource_kind: "*", resource_id: "*" };
    expect(
      selectorMatches(grant, {
        resource_kind: "project",
        resource_id: "proj_123",
      }),
    ).toBe(true);
    expect(
      selectorMatches(grant, {
        resource_kind: "mcp",
        resource_id: "tool_a",
      }),
    ).toBe(true);
  });

  it("disposition grant matches connection check without disposition", () => {
    const grant = {
      resource_kind: "mcp",
      resource_id: "*",
      disposition: "read_only",
    };
    const check = { resource_kind: "mcp", resource_id: "toolsetA" };
    expect(selectorMatches(grant, check)).toBe(true);
  });

  it("disposition grant denies wrong disposition", () => {
    const grant = {
      resource_kind: "mcp",
      resource_id: "*",
      disposition: "read_only",
    };
    expect(
      selectorMatches(grant, {
        resource_kind: "mcp",
        resource_id: "toolsetA",
        disposition: "read_only",
      }),
    ).toBe(true);
    expect(
      selectorMatches(grant, {
        resource_kind: "mcp",
        resource_id: "toolsetA",
        disposition: "destructive",
      }),
    ).toBe(false);
  });
});
