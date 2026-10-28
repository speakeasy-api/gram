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
      `https://${"a".repeat(2048)}`,
    ]) {
      expect(adoptionDashboardUrlProblem(bad)).not.toBeNull();
    }
  });
});
