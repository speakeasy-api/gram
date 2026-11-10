import { act, cleanup, fireEvent, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useOAuthPopup } from "./useOAuthPopup";

beforeEach(() => vi.useFakeTimers());
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.useRealTimers();
});

function setup() {
  const child = {
    location: { href: "about:blank" },
    closed: false,
    opener: window as Window | null,
    focus: vi.fn(),
    close: vi.fn(),
  };
  vi.spyOn(window, "open").mockReturnValue(child as unknown as Window);
  const completed = vi.fn<(reason: "popup-closed" | "focus") => void>();
  const { result } = renderHook(() => useOAuthPopup(completed));
  return { child, completed, result };
}

describe("asynchronous OAuth popup setup", () => {
  it("reserves a window immediately and consumes closure once after setup finishes", async () => {
    const { child, completed, result } = setup();
    const destination = Promise.withResolvers<string>();
    act(() => {
      result.current.open(() => destination.promise);
    });
    expect(window.open).toHaveBeenCalledTimes(1);
    expect(child.opener).toBeNull();
    expect(child.location.href).toBe("about:blank");
    child.closed = true;
    act(() => {
      fireEvent(window, new Event("focus"));
      vi.advanceTimersByTime(1000);
    });
    expect(completed).not.toHaveBeenCalled();
    await act(async () => {
      destination.resolve("https://example.test/oauth");
    });
    await act(async () => {
      vi.advanceTimersByTime(1000);
    });
    expect(completed).toHaveBeenCalledExactlyOnceWith("popup-closed");
    expect(child.location.href).toBe("about:blank");
  });

  it("navigates the reserved window only after setup succeeds", async () => {
    const { child, result } = setup();
    await act(async () => {
      result.current.open(
        async () => "https://example.test/oauth?attempt=receipt",
      );
    });
    expect(child.location.href).toBe(
      "https://example.test/oauth?attempt=receipt",
    );
  });

  it("does not start setup when a popup is blocked", () => {
    const { result } = setup();
    vi.mocked(window.open).mockReturnValue(null);
    const begin = vi.fn<() => Promise<string>>();
    act(() => {
      expect(result.current.open(begin)).toBe(false);
    });
    expect(begin).not.toHaveBeenCalled();
    expect(result.current.blocked).toBe(true);
  });

  it("closes a failed setup without reporting an OAuth completion", async () => {
    const { child, completed, result } = setup();
    await act(async () => {
      result.current.open(async () => {
        throw new Error("begin failed");
      });
    });
    expect(child.close).toHaveBeenCalledTimes(1);
    child.closed = true;
    await act(async () => {
      vi.advanceTimersByTime(1000);
    });
    expect(completed).not.toHaveBeenCalled();
  });
});

it("recovers from synchronous setup failure and allows another attempt", async () => {
  const { child, result, completed } = setup();
  await act(async () => {
    result.current.open(() => {
      throw new Error("setup failed");
    });
  });
  expect(child.close).toHaveBeenCalledOnce();
  expect(completed).not.toHaveBeenCalled();
  await act(async () => {
    expect(result.current.open(async () => "https://example.test/retry")).toBe(
      true,
    );
  });
  expect(child.location.href).toBe("https://example.test/retry");
});
