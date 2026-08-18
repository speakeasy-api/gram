import { renderHook } from "@testing-library/react";
import { act } from "react";
import { beforeEach, describe, expect, it } from "vitest";
import {
  dismissProjectGuide,
  markProjectGuideStarted,
  restoreProjectGuide,
  useProjectGuideDismissed,
  useProjectGuideStarted,
} from "./projectGuideStores";

beforeEach(() => {
  localStorage.clear();
});

describe("projectGuideStores", () => {
  it("reports not started and not dismissed for an untouched project", () => {
    const started = renderHook(() => useProjectGuideStarted("alpha"));
    const dismissed = renderHook(() => useProjectGuideDismissed("alpha"));
    expect(started.result.current).toBe(false);
    expect(dismissed.result.current).toBe(false);
  });

  it("marks a project as started and keeps other projects untouched", () => {
    const alpha = renderHook(() => useProjectGuideStarted("alpha"));
    const beta = renderHook(() => useProjectGuideStarted("beta"));
    act(() => markProjectGuideStarted("alpha"));
    expect(alpha.result.current).toBe(true);
    expect(beta.result.current).toBe(false);
  });

  it("clears the started flag on dismiss, so no run is left in progress", () => {
    const started = renderHook(() => useProjectGuideStarted("alpha"));
    const dismissed = renderHook(() => useProjectGuideDismissed("alpha"));
    act(() => markProjectGuideStarted("alpha"));
    act(() => dismissProjectGuide("alpha"));
    expect(dismissed.result.current).toBe(true);
    expect(started.result.current).toBe(false);
  });

  it("restores a dismissed guide", () => {
    const dismissed = renderHook(() => useProjectGuideDismissed("alpha"));
    act(() => dismissProjectGuide("alpha"));
    act(() => restoreProjectGuide("alpha"));
    expect(dismissed.result.current).toBe(false);
  });

  it("reads false when no project slug is known yet", () => {
    const started = renderHook(() => useProjectGuideStarted(undefined));
    expect(started.result.current).toBe(false);
  });
});
