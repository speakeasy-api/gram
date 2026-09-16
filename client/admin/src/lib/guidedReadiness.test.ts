import { describe, expect, it } from "vitest";

import {
  toGuidedReadiness,
  type GuidedReadinessResponse,
} from "@/lib/guidedReadiness";

const RESPONSE: GuidedReadinessResponse = {
  provider: "okta",
  eligible: false,
  checked_at: "2026-09-15T10:00:00Z",
  checks: [
    {
      key: "workos_organization_linked",
      ok: true,
      detail: "The organization is linked.",
      remedy: "",
      owner: "platform_admin",
      checked_at: "2026-09-15T09:59:00Z",
    },
    {
      key: "sso_feature_enabled",
      ok: false,
      detail: "Single sign-on is not enabled.",
      remedy: "Enable the feature on the organization.",
      owner: "speakeasy",
      checked_at: "2026-09-15T10:00:00Z",
    },
  ],
};

describe("toGuidedReadiness", () => {
  it("carries every field the panel draws", () => {
    const readiness = toGuidedReadiness(RESPONSE);

    expect(readiness.provider).toBe("okta");
    expect(readiness.eligible).toBe(false);
    expect(readiness.checks.map((check) => check.key)).toEqual([
      "workos_organization_linked",
      "sso_feature_enabled",
    ]);
    expect(readiness.checks[1]?.ok).toBe(false);
    expect(readiness.checks[1]?.detail).toBe("Single sign-on is not enabled.");
    expect(readiness.checks[1]?.remedy).toBe(
      "Enable the feature on the organization.",
    );
    expect(readiness.checks[1]?.owner).toBe("speakeasy");
  });

  // The wire carries timestamps as strings and the panel formats Dates, so the
  // conversion is the whole reason this mapping exists.
  it("reads the timestamps as dates", () => {
    const readiness = toGuidedReadiness(RESPONSE);

    expect(readiness.checkedAt.toISOString()).toBe("2026-09-15T10:00:00.000Z");
    expect(readiness.checks[0]?.checkedAt.toISOString()).toBe(
      "2026-09-15T09:59:00.000Z",
    );
  });
});
