import { describe, expect, it } from "vitest";
import { ONBOARDING_TASK_IDS } from "./onboarding-tasks";
import {
  canonicalSetupSearch,
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
    const slugs = ONBOARDING_TASK_IDS.map(setupTaskSlug);
    expect(new Set([...slugs, ...ONBOARDING_TASK_IDS]).size).toBe(
      // An alias must never collide with another task's key.
      new Set(ONBOARDING_TASK_IDS).size +
        slugs.filter((slug, index) => slug !== ONBOARDING_TASK_IDS[index])
          .length,
    );
    for (const key of ONBOARDING_TASK_IDS) {
      expect(setupTaskKeyForSlug(setupTaskSlug(key))).toBe(key);
      expect(setupTaskKeyForSlug(key)).toBe(key);
    }
  });

  it("uses the requested short slugs", () => {
    expect(setupTaskSlug("identity-provider")).toBe("idp");
    expect(setupTaskSlug("domain-verification")).toBe("domain");
    expect(setupTaskSlug("instrument-agents")).toBe("other-platforms");
    expect(setupTaskSlug("additional-agent-config")).toBe("integrations");
    expect(setupTaskSlug("configure-policies")).toBe("policies");
    expect(setupTaskSlug("anthropic-observability")).toBe(
      "anthropic-observability",
    );
  });

  it("still resolves a task key used as a slug, and nothing else", () => {
    expect(setupTaskKeyForSlug("identity-provider")).toBe("identity-provider");
    expect(setupTaskKeyForSlug("nope")).toBeUndefined();
  });
});
