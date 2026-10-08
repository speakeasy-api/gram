import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import {
  pinnedCustomersStorageKey,
  readPinnedCustomers,
  usePinnedCustomers,
} from "./pinnedCustomers";

const ALICE = "alice@example.com";
const BOB = "bob@example.com";

describe("pinned customers storage", () => {
  afterEach(() => {
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it("remembers pins across visits", () => {
    const { result } = renderHook(() => usePinnedCustomers(ALICE));
    act(() => result.current.togglePinned("org_a"));
    act(() => result.current.togglePinned("org_b"));
    act(() => result.current.togglePinned("org_a"));
    expect([...result.current.pinned]).toEqual(["org_b"]);
    expect(readPinnedCustomers(ALICE)).toEqual(["org_b"]);

    const { result: nextVisit } = renderHook(() => usePinnedCustomers(ALICE));
    expect(nextVisit.current.pinned.has("org_b")).toBe(true);
  });

  it("keeps each operator's pins separate on a shared browser", () => {
    const { result: alice } = renderHook(() => usePinnedCustomers(ALICE));
    act(() => alice.current.togglePinned("org_a"));

    const { result: bob } = renderHook(() => usePinnedCustomers(BOB));
    expect(bob.current.pinned.size).toBe(0);
    act(() => bob.current.togglePinned("org_b"));
    expect(readPinnedCustomers(ALICE)).toEqual(["org_a"]);
    expect(readPinnedCustomers(BOB)).toEqual(["org_b"]);
  });

  it("reads the new operator's pins when the viewer changes", () => {
    localStorage.setItem(
      pinnedCustomersStorageKey(BOB),
      JSON.stringify(["org_b"]),
    );
    const { result, rerender } = renderHook(
      ({ viewer }) => usePinnedCustomers(viewer),
      { initialProps: { viewer: ALICE } },
    );
    act(() => result.current.togglePinned("org_a"));
    rerender({ viewer: BOB });
    expect([...result.current.pinned]).toEqual(["org_b"]);
    expect(readPinnedCustomers(ALICE)).toEqual(["org_a"]);
  });

  it("writes once per change, not when it first reads", () => {
    const setItem = vi.spyOn(localStorage, "setItem");
    const { result } = renderHook(() => usePinnedCustomers(ALICE));
    expect(setItem).not.toHaveBeenCalled();
    act(() => result.current.togglePinned("org_a"));
    expect(setItem).toHaveBeenCalledTimes(1);
  });

  it("ignores corrupt stored values", () => {
    localStorage.setItem(pinnedCustomersStorageKey(ALICE), "{not json");
    expect(readPinnedCustomers(ALICE)).toEqual([]);
    localStorage.setItem(
      pinnedCustomersStorageKey(ALICE),
      JSON.stringify(["org_a", 7, null]),
    );
    expect(readPinnedCustomers(ALICE)).toEqual(["org_a"]);
  });

  it("still pins for this visit when storage is unavailable or the viewer is unknown", () => {
    vi.spyOn(localStorage, "getItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    vi.spyOn(localStorage, "setItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    const { result } = renderHook(() => usePinnedCustomers(ALICE));
    act(() => result.current.togglePinned("org_a"));
    expect(result.current.pinned.has("org_a")).toBe(true);

    const { result: anonymous } = renderHook(() =>
      usePinnedCustomers(undefined),
    );
    act(() => anonymous.current.togglePinned("org_a"));
    expect(anonymous.current.pinned.has("org_a")).toBe(true);
  });
});
