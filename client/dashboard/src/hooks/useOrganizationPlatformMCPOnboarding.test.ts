import type { GramCore } from "@gram/client/core.js";
import { describe, expect, it } from "vitest";
import { buildOrganizationPlatformMCPOnboardingQuery } from "./useOrganizationPlatformMCPOnboarding";

describe("buildOrganizationPlatformMCPOnboardingQuery", () => {
  it("isolates onboarding state by organization", () => {
    const client = {} as GramCore;

    const first = buildOrganizationPlatformMCPOnboardingQuery(
      client,
      "org-first",
    );
    const second = buildOrganizationPlatformMCPOnboardingQuery(
      client,
      "org-second",
    );

    expect(first.queryKey).not.toEqual(second.queryKey);
    expect(first.queryKey).toEqual([
      "@gram/client",
      "platformMcp",
      "getOnboarding",
      { gramSession: "" },
      { organizationId: "org-first" },
    ]);
  });
});
