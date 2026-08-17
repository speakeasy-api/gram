import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { createDismissedCtaStore } from "./useDismissedCtaStore";

describe("createDismissedCtaStore", () => {
  it("shares a scoped dismissal across hook consumers", () => {
    const store = createDismissedCtaStore("test-dismissed-cta");
    const first = renderHook(() => store.useDismissed("user:organization"));
    const second = renderHook(() => store.useDismissed("user:organization"));

    expect(first.result.current).toBe(false);
    expect(second.result.current).toBe(false);

    act(() => store.write("user:organization", true));

    expect(first.result.current).toBe(true);
    expect(second.result.current).toBe(true);
  });

  it("keeps dismissal scopes isolated", () => {
    const store = createDismissedCtaStore("test-dismissed-cta-isolation");
    const first = renderHook(() => store.useDismissed("user-one:organization"));
    const second = renderHook(() =>
      store.useDismissed("user-two:organization"),
    );

    act(() => store.write("user-one:organization", true));

    expect(first.result.current).toBe(true);
    expect(second.result.current).toBe(false);
  });

  it("responds to a dismissal from another browser tab", () => {
    const prefix = "test-dismissed-cta-storage";
    const store = createDismissedCtaStore(prefix);
    const hook = renderHook(() => store.useDismissed("user:organization"));

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
});
