import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useModeSwitcherEnabled } from "./mode-switch-context";

const state = vi.hoisted(() => ({ hasScope: vi.fn() }));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: state.hasScope }),
}));

describe("useModeSwitcherEnabled", () => {
  beforeEach(() => state.hasScope.mockReset());

  it("shows the switcher for organization admins", () => {
    state.hasScope.mockImplementation((scope) => scope === "org:admin");
    const { result } = renderHook(() => useModeSwitcherEnabled());
    expect(result.current).toBe(true);
  });

  it("keeps the switcher hidden for project-only administrators", () => {
    state.hasScope.mockImplementation((scope) => scope === "project:admin");
    const { result } = renderHook(() => useModeSwitcherEnabled());
    expect(result.current).toBe(false);
  });

  it("hides the switcher when admin access is removed", () => {
    state.hasScope.mockReturnValue(true);
    const { result, rerender } = renderHook(() => useModeSwitcherEnabled());
    state.hasScope.mockReturnValue(false);
    rerender();
    expect(result.current).toBe(false);
  });
});
