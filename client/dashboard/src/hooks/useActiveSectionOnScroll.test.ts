import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useActiveSectionOnScroll } from "./useActiveSectionOnScroll";

type ObserverCallback = (entries: unknown[]) => void;

const observerCallbacks: ObserverCallback[] = [];

class MockIntersectionObserver {
  callback: ObserverCallback;
  constructor(callback: ObserverCallback) {
    this.callback = callback;
    observerCallbacks.push(callback);
  }
  observe() {}
  unobserve() {}
  disconnect() {}
}

// Drives every live observer so a simulated scroll triggers a recompute, the
// same signal a real IntersectionObserver delivers when a section crosses the
// activation line.
function fireObservers() {
  for (const callback of observerCallbacks) {
    callback([]);
  }
}

const TOP_OFFSET = 120;

// Each section's top edge in viewport coordinates; adjusting these simulates
// the page scrolling under a fixed activation line.
const sectionTops = new Map<string, number>();

function setSectionTops(tops: Record<string, number>) {
  sectionTops.clear();
  for (const [id, top] of Object.entries(tops)) {
    sectionTops.set(id, top);
  }
}

function createSection(id: string) {
  const el = document.createElement("section");
  el.id = id;
  Object.defineProperty(el, "getBoundingClientRect", {
    configurable: true,
    value: () => ({ top: sectionTops.get(id) ?? 0 }) as DOMRect,
  });
  document.body.appendChild(el);
}

// The hook batches recomputes on an animation frame. Queue them so the handle
// is returned before the callback runs (matching a real async rAF), then flush
// on demand inside act().
let frameQueue: FrameRequestCallback[] = [];
let nextFrameHandle = 1;

function flushFrames() {
  const pending = frameQueue;
  frameQueue = [];
  for (const cb of pending) {
    cb(0);
  }
}

beforeEach(() => {
  observerCallbacks.length = 0;
  frameQueue = [];
  nextFrameHandle = 1;
  vi.stubGlobal("IntersectionObserver", MockIntersectionObserver);
  vi.stubGlobal("requestAnimationFrame", (cb: FrameRequestCallback) => {
    frameQueue.push(cb);
    return nextFrameHandle++;
  });
  vi.stubGlobal("cancelAnimationFrame", () => {});
});

afterEach(() => {
  document.body.innerHTML = "";
  sectionTops.clear();
  vi.unstubAllGlobals();
});

describe("useActiveSectionOnScroll", () => {
  it("activates the first section while the page is scrolled to the top", () => {
    createSection("a");
    createSection("b");
    createSection("c");
    // Every section starts below the activation line.
    setSectionTops({ a: TOP_OFFSET + 40, b: 600, c: 1200 });

    const { result } = renderHook(() =>
      useActiveSectionOnScroll(["a", "b", "c"], { topOffset: TOP_OFFSET }),
    );
    act(() => flushFrames());

    expect(result.current).toBe("a");
  });

  it("advances the active section as later sections cross the activation line", () => {
    createSection("a");
    createSection("b");
    createSection("c");
    setSectionTops({ a: -400, b: TOP_OFFSET - 5, c: 800 });

    const { result } = renderHook(() =>
      useActiveSectionOnScroll(["a", "b", "c"], { topOffset: TOP_OFFSET }),
    );
    act(() => flushFrames());

    // "b" has scrolled just above the line; "c" is still below it.
    expect(result.current).toBe("b");

    // Scroll further so "c" crosses the line too.
    act(() => {
      setSectionTops({ a: -1200, b: -400, c: TOP_OFFSET - 5 });
      fireObservers();
      flushFrames();
    });

    expect(result.current).toBe("c");
  });

  it("re-subscribes when the set of sections changes", () => {
    createSection("a");
    createSection("b");
    setSectionTops({ a: -400, b: -10 });

    const { result, rerender } = renderHook(
      ({ ids }: { ids: string[] }) =>
        useActiveSectionOnScroll(ids, { topOffset: TOP_OFFSET }),
      { initialProps: { ids: ["a", "b"] } },
    );
    act(() => flushFrames());

    expect(result.current).toBe("b");

    // A new trailing section appears above the line after more content loads.
    createSection("c");
    setSectionTops({ a: -1200, b: -400, c: TOP_OFFSET - 5 });
    // Re-run the effect first so it re-subscribes, then flush its queued frame.
    act(() => rerender({ ids: ["a", "b", "c"] }));
    act(() => flushFrames());

    expect(result.current).toBe("c");
  });

  // The sidebar that calls this hook mounts before the page content that owns
  // the sections, so observing only what exists on the first pass would freeze
  // the highlight on the first section forever.
  it("starts tracking sections that mount after the hook runs", async () => {
    const { result } = renderHook(() =>
      useActiveSectionOnScroll(["a", "b", "c"], { topOffset: TOP_OFFSET }),
    );
    act(() => flushFrames());

    // No sections in the DOM yet.
    expect(observerCallbacks.length).toBe(0);

    createSection("a");
    createSection("b");
    createSection("c");
    setSectionTops({ a: -400, b: TOP_OFFSET - 5, c: 800 });

    // MutationObserver callbacks are delivered as a microtask.
    await act(async () => {
      await Promise.resolve();
      flushFrames();
    });

    expect(observerCallbacks.length).toBeGreaterThan(0);
    expect(result.current).toBe("b");
  });

  // The trailing sections share the last screenful, so scrolling can never
  // bring them to the activation line — without the pin the highlight springs
  // back to whichever earlier section sits on the line.
  it("keeps a requested section active when scrolling cannot reach it", () => {
    createSection("a");
    createSection("b");
    createSection("c");
    // Only "a" is above the line; "b" and "c" can never get there.
    setSectionTops({ a: -10, b: 500, c: 700 });

    const { result } = renderHook(() =>
      useActiveSectionOnScroll(["a", "b", "c"], {
        topOffset: TOP_OFFSET,
        requestedSectionId: "c",
        requestKey: "nav-1",
      }),
    );
    act(() => flushFrames());

    expect(result.current).toBe("c");
  });

  it("releases the requested section once the reader scrolls", () => {
    createSection("a");
    createSection("b");
    createSection("c");
    setSectionTops({ a: -10, b: 500, c: 700 });

    const { result } = renderHook(() =>
      useActiveSectionOnScroll(["a", "b", "c"], {
        topOffset: TOP_OFFSET,
        requestedSectionId: "c",
        requestKey: "nav-1",
      }),
    );
    act(() => flushFrames());
    expect(result.current).toBe("c");

    // Typing is not scrolling, so the pin survives it.
    act(() => {
      window.dispatchEvent(new KeyboardEvent("keydown", { key: "k" }));
    });
    expect(result.current).toBe("c");

    act(() => {
      window.dispatchEvent(new WheelEvent("wheel", { deltaY: 40 }));
    });

    // Scroll tracking takes over again.
    expect(result.current).toBe("a");
  });

  it("re-pins the same section when its link is clicked again", () => {
    createSection("a");
    createSection("b");
    setSectionTops({ a: -10, b: 500 });

    const { result, rerender } = renderHook(
      ({ requestKey }: { requestKey: string }) =>
        useActiveSectionOnScroll(["a", "b"], {
          topOffset: TOP_OFFSET,
          requestedSectionId: "b",
          requestKey,
        }),
      { initialProps: { requestKey: "nav-1" } },
    );
    act(() => flushFrames());
    expect(result.current).toBe("b");

    act(() => {
      window.dispatchEvent(new WheelEvent("wheel", { deltaY: 40 }));
    });
    expect(result.current).toBe("a");

    // Same hash, new navigation — the section should be pinned again.
    act(() => rerender({ requestKey: "nav-2" }));

    expect(result.current).toBe("b");
  });
});
