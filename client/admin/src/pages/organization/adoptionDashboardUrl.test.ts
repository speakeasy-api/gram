import { describe, expect, it } from "vitest";

import { adoptionDashboardUrlProblem } from "@/pages/organization/adoptionDashboardUrl";

describe("adoptionDashboardUrlProblem", () => {
  it("accepts an empty value, which clears the link", () => {
    expect(adoptionDashboardUrlProblem("")).toBeNull();
    expect(adoptionDashboardUrlProblem("   ")).toBeNull();
  });

  it("accepts an https link", () => {
    expect(
      adoptionDashboardUrlProblem(" https://claude.ai/artifact/example "),
    ).toBeNull();
  });

  it("rejects links the server would refuse", () => {
    for (const bad of [
      "http://claude.ai/artifact/example",
      "claude.ai/artifact/example",
      "javascript:alert(1)",
      "https://user:pass@claude.ai/artifact/example",
      "https:example.com",
      "https:/example.com",
      "https://:443",
      `https://${"a".repeat(2048)}`,
    ]) {
      expect(adoptionDashboardUrlProblem(bad)).not.toBeNull();
    }
  });

  it("accepts an uppercase scheme", () => {
    expect(adoptionDashboardUrlProblem("HTTPS://example.com/a")).toBeNull();
  });

  it("counts characters, not UTF-16 units, against the 2048 limit", () => {
    const prefix = "https://example.com/";
    const atLimit = prefix + "a".repeat(2048 - prefix.length);
    expect(atLimit).toHaveLength(2048);
    expect(adoptionDashboardUrlProblem(` ${atLimit} `)).toBeNull();
    expect(adoptionDashboardUrlProblem(`${atLimit}a`)).not.toBeNull();

    // An astral character is two UTF-16 units but one character.
    const astral = prefix + "\u{1F600}".repeat(2048 - prefix.length);
    expect(Array.from(astral)).toHaveLength(2048);
    expect(adoptionDashboardUrlProblem(astral)).toBeNull();
    expect(adoptionDashboardUrlProblem(`${astral}a`)).not.toBeNull();
  });
});
