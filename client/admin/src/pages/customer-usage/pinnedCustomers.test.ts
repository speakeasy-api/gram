import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { readPinnedCustomers, usePinnedCustomers } from "./pinnedCustomers";

const KEY = "gram-admin:customer-usage:pinned";

describe("pinned customers storage", () => {
  afterEach(() => {
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it("remembers pins across visits", () => {
    const { result } = renderHook(() => usePinnedCustomers());
    act(() => result.current.togglePinned("org_a"));
    act(() => result.current.togglePinned("org_b"));
    act(() => result.current.togglePinned("org_a"));
    expect([...result.current.pinned]).toEqual(["org_b"]);
    expect(readPinnedCustomers()).toEqual(["org_b"]);

    const { result: nextVisit } = renderHook(() => usePinnedCustomers());
    expect(nextVisit.current.pinned.has("org_b")).toBe(true);
  });

  it("ignores corrupt stored values", () => {
    localStorage.setItem(KEY, "{not json");
    expect(readPinnedCustomers()).toEqual([]);
    localStorage.setItem(KEY, JSON.stringify(["org_a", 7, null]));
    expect(readPinnedCustomers()).toEqual(["org_a"]);
  });

  it("still pins for this visit when storage is unavailable", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    const { result } = renderHook(() => usePinnedCustomers());
    act(() => result.current.togglePinned("org_a"));
    expect(result.current.pinned.has("org_a")).toBe(true);
  });
});
