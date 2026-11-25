import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { onboardingPresets } from "./onboardingPresets";

describe("onboarding presets", () => {
  it("references only steps in the server catalog", () => {
    const catalog = readFileSync(
      resolve("../../server/internal/organizations/setup_tasks.go"),
      "utf8",
    );
    const slugs = new Set(
      [...catalog.matchAll(/Key: "([^"]+)"/g)].map((match) => match[1]),
    );
    for (const preset of onboardingPresets) {
      for (const slug of preset.stepSlugs) {
        expect(slugs.has(slug), `${preset.id}: ${slug}`).toBe(true);
      }
    }
  });
});
