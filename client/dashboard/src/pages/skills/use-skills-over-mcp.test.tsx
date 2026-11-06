import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  useSkillsOverMcpEnabled,
  useSkillsOverMcpStatus,
} from "./use-skills-over-mcp";

const state = vi.hoisted(() => ({
  query: {
    isPending: false,
    isError: false,
    data: { skillsOverMcpEnabled: false } as
      | { skillsOverMcpEnabled: boolean }
      | undefined,
  },
  calls: [] as unknown[][],
}));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-under-test" }),
}));

vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  useProductFeatures: (...args: unknown[]) => {
    state.calls.push(args);
    return state.query;
  },
}));

describe("useSkillsOverMcpStatus", () => {
  beforeEach(() => {
    state.calls = [];
    state.query = {
      isPending: false,
      isError: false,
      data: { skillsOverMcpEnabled: false },
    };
  });

  it("queries product features for the current organization without throwing", () => {
    renderHook(() => useSkillsOverMcpStatus());
    expect(state.calls[0]?.[0]).toEqual({ organizationId: "org-under-test" });
    expect(state.calls[0]?.[2]).toMatchObject({ throwOnError: false });
  });

  it("is enabled only when the product feature is on", () => {
    state.query.data = { skillsOverMcpEnabled: true };
    expect(renderHook(() => useSkillsOverMcpStatus()).result.current).toEqual({
      status: "enabled",
    });
    expect(renderHook(() => useSkillsOverMcpEnabled()).result.current).toBe(
      true,
    );
  });

  it("is disabled when the product feature is off", () => {
    expect(renderHook(() => useSkillsOverMcpStatus()).result.current).toEqual({
      status: "disabled",
    });
    expect(renderHook(() => useSkillsOverMcpEnabled()).result.current).toBe(
      false,
    );
  });

  it("reports loading while the query is pending", () => {
    state.query = { isPending: true, isError: false, data: undefined };
    expect(renderHook(() => useSkillsOverMcpStatus()).result.current).toEqual({
      status: "loading",
    });
    expect(renderHook(() => useSkillsOverMcpEnabled()).result.current).toBe(
      false,
    );
  });

  it("reports an error when the query fails", () => {
    state.query = { isPending: false, isError: true, data: undefined };
    expect(renderHook(() => useSkillsOverMcpStatus()).result.current).toEqual({
      status: "error",
    });
    expect(renderHook(() => useSkillsOverMcpEnabled()).result.current).toBe(
      false,
    );
  });
});
