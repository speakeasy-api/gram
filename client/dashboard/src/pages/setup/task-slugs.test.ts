import { describe, expect, it } from "vitest";
import {
  canonicalSetupSearch,
  SETUP_TASK_SLUGS,
  setupTaskKeyForSlug,
  setupTaskSlug,
} from "./task-slugs";

describe("setup task slugs", () => {
  it("converts legacy step-only task links", () => {
    expect(
      canonicalSetupSearch(
        new URLSearchParams("step=connect-idp&view=workstreams"),
      ).toString(),
    ).toBe("view=workstreams&task=connect-idp");
  });

  it.each(["enable-logging", "confirm-traffic"])(
    "preserves the %s sub-step of an explicit task",
    (step) => {
      const search = new URLSearchParams({
        view: "workstreams",
        task: "anthropic-observability",
        step,
      });
      expect(canonicalSetupSearch(search).toString()).toBe(search.toString());
    },
  );
  it("maps every task to a distinct slug and back", () => {
    const slugs = Object.values(SETUP_TASK_SLUGS);
    expect(new Set(slugs).size).toBe(slugs.length);
    for (const [key, slug] of Object.entries(SETUP_TASK_SLUGS)) {
      expect(setupTaskSlug(key)).toBe(slug);
      expect(setupTaskKeyForSlug(slug)).toBe(key);
    }
  });

  it("uses the requested short slugs", () => {
    expect(setupTaskSlug("identity-provider")).toBe("idp");
    expect(setupTaskSlug("anthropic-observability")).toBe(
      "anthropic-observability",
    );
  });

  it("still resolves a task key used as a slug, and nothing else", () => {
    expect(setupTaskKeyForSlug("identity-provider")).toBe("identity-provider");
    expect(setupTaskKeyForSlug("nope")).toBeUndefined();
  });
});
