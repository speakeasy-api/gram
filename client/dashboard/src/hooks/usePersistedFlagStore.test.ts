import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { createPersistedFlagStore } from "./usePersistedFlagStore";

describe("createPersistedFlagStore", () => {
  it("shares a scoped dismissal across hook consumers", () => {
    const store = createPersistedFlagStore("test-dismissed-cta");
    const first = renderHook(() => store.useFlag("user:organization"));
    const second = renderHook(() => store.useFlag("user:organization"));

    expect(first.result.current).toBe(false);
    expect(second.result.current).toBe(false);

    act(() => store.write("user:organization", true));

    expect(first.result.current).toBe(true);
    expect(second.result.current).toBe(true);
  });

  it("keeps dismissal scopes isolated", () => {
    const store = createPersistedFlagStore("test-dismissed-cta-isolation");
    const first = renderHook(() => store.useFlag("user-one:organization"));
    const second = renderHook(() => store.useFlag("user-two:organization"));

    act(() => store.write("user-one:organization", true));

    expect(first.result.current).toBe(true);
    expect(second.result.current).toBe(false);
  });

  it("responds to a dismissal from another browser tab", () => {
    const prefix = "test-dismissed-cta-storage";
    const store = createPersistedFlagStore(prefix);
    const hook = renderHook(() => store.useFlag("user:organization"));

    act(() => {
      window.dispatchEvent(
        new StorageEvent("storage", {
          key: `${prefix}:user:organization`,
          newValue: "true",
        }),
      );
    });

    expect(hook.result.current).toBe(true);
  });

  it("clears in-memory dismissal after storage is cleared in another tab", () => {
    const store = createPersistedFlagStore("test-dismissed-cta-storage-clear");
    const hook = renderHook(() => store.useFlag("user:organization"));
    act(() => store.write("user:organization", true));

    act(() => {
      localStorage.clear();
      window.dispatchEvent(new StorageEvent("storage", { key: null }));
    });

    expect(hook.result.current).toBe(false);
  });
});
