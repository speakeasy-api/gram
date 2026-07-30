import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { useOrgMemoryDeveloperToggle } from "./useOrgMemoryDeveloperToggle";

afterEach(() => {
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
});
