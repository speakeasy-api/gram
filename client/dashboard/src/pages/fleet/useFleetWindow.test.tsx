import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { FLEET_WINDOW_MS } from "./fleet-model";
import { useFleetWindow } from "./useFleetWindow";

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.restoreAllMocks();
});

it("rolls the server pagination bound and pauses while hidden, refreshing on resume", () => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date("2026-09-01T12:00:00Z"));
  const visibility = vi
    .spyOn(document, "visibilityState", "get")
    .mockReturnValue("visible");
  const { result } = renderHook(useFleetWindow);
  expect(result.current.from.getTime()).toBe(Date.now() - FLEET_WINDOW_MS);
  void act(() => vi.advanceTimersByTime(30_000));
  expect(result.current.from.getTime()).toBe(Date.now() - FLEET_WINDOW_MS);
  const lastVisible = result.current.from;
  visibility.mockReturnValue("hidden");
  void act(() => vi.advanceTimersByTime(90_000));
  expect(result.current.from).toBe(lastVisible);
  visibility.mockReturnValue("visible");
  act(() => {
    document.dispatchEvent(new Event("visibilitychange"));
  });
  expect(result.current.from.getTime()).toBe(Date.now() - FLEET_WINDOW_MS);
  act(() => {
    vi.setSystemTime(Date.now() + 90_000);
    window.dispatchEvent(new Event("focus"));
  });
  expect(result.current.from.getTime()).toBe(Date.now() - FLEET_WINDOW_MS);
});
