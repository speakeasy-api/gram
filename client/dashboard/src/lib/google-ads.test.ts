import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createGoogleAds } from "./google-ads";

// happy-dom refuses to fetch external scripts (and logs loudly when one is
// connected), so record what would have been appended instead of appending.
const injected: HTMLScriptElement[] = [];

function injectedScripts(): HTMLScriptElement[] {
  return injected.filter((script) =>
    script.src.startsWith("https://www.googletagmanager.com/gtag/js"),
  );
}

function queuedCommands(): unknown[][] {
  return (window.dataLayer ?? []).map((entry) =>
    Array.from(entry as ArrayLike<unknown>),
  );
}

describe("createGoogleAds", () => {
  beforeEach(() => {
    delete window.dataLayer;
    delete window.gtag;
    injected.length = 0;
    vi.spyOn(document.head, "appendChild").mockImplementation((node) => {
      if (node instanceof HTMLScriptElement) {
        injected.push(node);
      }
      return node;
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  it("does nothing without a tag id", () => {
    const ads = createGoogleAds({ tagId: "", enabled: () => true });
    const onComplete = vi.fn<() => void>();

    expect(ads.initialize()).toBe(false);
    ads.trackConversion("platform_signup", undefined, onComplete);

    expect(injectedScripts()).toHaveLength(0);
    expect(window.dataLayer).toBeUndefined();
    expect(onComplete).toHaveBeenCalledOnce();
  });

  it("does nothing when disabled for this page load", () => {
    const ads = createGoogleAds({ tagId: "AW-1", enabled: () => false });
    const onComplete = vi.fn<() => void>();

    ads.trackConversion("platform_signup", undefined, onComplete);

    expect(injectedScripts()).toHaveLength(0);
    expect(window.gtag).toBeUndefined();
    expect(onComplete).toHaveBeenCalledOnce();
  });

  it("injects gtag.js once and queues the config ahead of events", () => {
    const ads = createGoogleAds({ tagId: "AW-1", enabled: () => true });

    expect(ads.initialize()).toBe(true);
    expect(ads.initialize()).toBe(true);
    ads.trackConversion("platform_signup", { product: "AI Control Plane" });

    const scripts = injectedScripts();
    expect(scripts).toHaveLength(1);
    expect(scripts[0]?.src).toBe(
      "https://www.googletagmanager.com/gtag/js?id=AW-1",
    );
    expect(scripts[0]?.async).toBe(true);

    // gtag.js replays `arguments` objects from the queue, so each entry must
    // be one, not an array.
    for (const entry of window.dataLayer ?? []) {
      expect(Object.prototype.toString.call(entry)).toBe("[object Arguments]");
    }
    const queue = queuedCommands();
    expect(queue[0]?.[0]).toBe("js");
    expect(queue[0]?.[1]).toBeInstanceOf(Date);
    expect(queue[1]).toEqual(["config", "AW-1"]);
    expect(queue[2]).toEqual([
      "event",
      "platform_signup",
      { product: "AI Control Plane" },
    ]);
  });

  it("runs onComplete once, from gtag's callback or the timeout, whichever comes first", () => {
    vi.useFakeTimers();
    const ads = createGoogleAds({ tagId: "AW-1", enabled: () => true });
    const onComplete = vi.fn<() => void>();

    ads.trackConversion("platform_signup", undefined, onComplete);

    const event = queuedCommands().at(-1);
    const params = event?.[2] as {
      event_callback: () => void;
      event_timeout: number;
    };
    expect(params.event_timeout).toBe(1000);
    expect(onComplete).not.toHaveBeenCalled();

    params.event_callback();
    expect(onComplete).toHaveBeenCalledOnce();

    vi.advanceTimersByTime(2000);
    expect(onComplete).toHaveBeenCalledOnce();
  });

  it("falls back to the timeout when gtag never answers", () => {
    vi.useFakeTimers();
    const ads = createGoogleAds({ tagId: "AW-1", enabled: () => true });
    const onComplete = vi.fn<() => void>();

    ads.trackConversion("platform_signup", undefined, onComplete);
    expect(onComplete).not.toHaveBeenCalled();

    vi.advanceTimersByTime(1000);
    expect(onComplete).toHaveBeenCalledOnce();
  });
});
