import { act, cleanup, fireEvent, renderHook } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useOAuthPopup } from "./useOAuthPopup";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.useRealTimers();
});

it.each(["focus", "poll"])(
  "consumes a popup close once when %s observes it first",
  async (first) => {
    vi.useFakeTimers();
    const child = {
      closed: false,
      location: { href: "" },
      opener: window,
      focus: vi.fn(),
    };
    vi.spyOn(window, "open").mockReturnValue(child as unknown as Window);
    let finish: () => void = () => {};
    const callback = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          finish = resolve;
        }),
    );
    const { result } = renderHook(() => useOAuthPopup(callback));
    act(() => {
      result.current.open("https://example.test/connect");
    });
    child.closed = true;
    if (first === "focus") fireEvent(window, new Event("focus"));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(500);
    });
    fireEvent(window, new Event("focus"));
    fireEvent(document, new Event("visibilitychange"));
    expect(callback).toHaveBeenCalledTimes(1);
    expect(callback).toHaveBeenCalledWith("popup-closed");
    await act(async () => {
      finish();
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000);
    });
    expect(callback).toHaveBeenCalledTimes(1);
    fireEvent(window, new Event("focus"));
    expect(callback).toHaveBeenLastCalledWith("focus");
    await act(async () => {
      finish();
    });
  },
);
