import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useOrgMemoryDeveloperToggle } from "./useOrgMemoryDeveloperToggle";

afterEach(() => {
  vi.restoreAllMocks();
  sessionStorage.clear();
});

describe("useOrgMemoryDeveloperToggle", () => {
  it("defaults off and synchronizes session changes between consumers", () => {
    const first = renderHook(() => useOrgMemoryDeveloperToggle());
    const second = renderHook(() => useOrgMemoryDeveloperToggle());

    expect(first.result.current[0]).toBe(false);
    expect(second.result.current[0]).toBe(false);

    act(() => first.result.current[1](true));

    expect(first.result.current[0]).toBe(true);
    expect(second.result.current[0]).toBe(true);
    expect(sessionStorage.getItem("gram-dev-org-memory")).toBe("1");

    act(() => second.result.current[1](false));

    expect(first.result.current[0]).toBe(false);
    expect(second.result.current[0]).toBe(false);
    expect(sessionStorage.getItem("gram-dev-org-memory")).toBeNull();
  });

  it("uses an in-memory fallback when session storage is unavailable", () => {
    const getItem = vi
      .spyOn(Storage.prototype, "getItem")
      .mockImplementation(() => {
        throw new Error("storage unavailable");
      });
    const setItem = vi
      .spyOn(Storage.prototype, "setItem")
      .mockImplementation(() => {
        throw new Error("storage unavailable");
      });
    const result = renderHook(() => useOrgMemoryDeveloperToggle());

    act(() => result.result.current[1](true));
    expect(result.result.current[0]).toBe(true);

    getItem.mockRestore();
    setItem.mockRestore();
    act(() => result.result.current[1](false));
  });
});
