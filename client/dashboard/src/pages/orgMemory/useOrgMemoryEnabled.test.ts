import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useOrgMemoryStatus } from "./useOrgMemoryEnabled";

const overview = vi.fn();
const rbac = vi.fn();

vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project-1" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => rbac(),
}));
vi.mock("@gram/client/react-query/orgMemoryOverview.js", () => ({
  useOrgMemoryOverview: (...args: unknown[]) => overview(...args),
}));

function grants(canRead: boolean, isLoading = false) {
  rbac.mockReturnValue({ isLoading, hasAnyScope: () => canRead });
}

describe("useOrgMemoryStatus", () => {
  beforeEach(() => {
    overview.mockReturnValue({ isPending: false, error: null });
  });

  it("is enabled when the overview answers", () => {
    grants(true);
    expect(renderHook(() => useOrgMemoryStatus()).result.current.status).toBe(
      "enabled",
    );
  });

  it("is disabled when the server answers 404", () => {
    grants(true);
    overview.mockReturnValue({
      isPending: false,
      error: Object.assign(new Error("not enabled"), { statusCode: 404 }),
    });
    expect(renderHook(() => useOrgMemoryStatus()).result.current.status).toBe(
      "disabled",
    );
  });

  it("stays enabled on other errors so the page can show them", () => {
    grants(true);
    overview.mockReturnValue({
      isPending: false,
      error: Object.assign(new Error("boom"), { statusCode: 500 }),
    });
    expect(renderHook(() => useOrgMemoryStatus()).result.current.status).toBe(
      "enabled",
    );
  });

  it("is disabled without memory:read and skips the request", () => {
    grants(false);
    expect(renderHook(() => useOrgMemoryStatus()).result.current.status).toBe(
      "disabled",
    );
    expect(overview).toHaveBeenLastCalledWith(
      { days: 30 },
      undefined,
      expect.objectContaining({ enabled: false }),
    );
  });

  it("is loading while grants load", () => {
    grants(false, true);
    expect(renderHook(() => useOrgMemoryStatus()).result.current.status).toBe(
      "loading",
    );
  });
});
