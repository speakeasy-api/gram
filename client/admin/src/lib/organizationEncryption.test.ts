// @vitest-environment node
import { describe, expect, it } from "vitest";

import {
  formatDuration,
  labelForUseCase,
  transitionPhase,
} from "@/lib/organizationEncryption";

describe("transitionPhase", () => {
  it("reads org_kms without a cleared time as clearing incomplete", () => {
    expect(transitionPhase("org_kms", undefined)).toBe("clearing");
    expect(transitionPhase("org_kms", new Date())).toBe("finalized");
  });

  it("reads the other states as themselves", () => {
    expect(transitionPhase("dual_write", undefined)).toBe("dual_write");
    expect(transitionPhase("platform_key", undefined)).toBe("platform_key");
  });
});

describe("formatDuration", () => {
  it("writes soak times the way staff say them", () => {
    expect(formatDuration(24 * 3600)).toBe("24 hours");
    expect(formatDuration(72 * 3600)).toBe("3 days");
    expect(formatDuration(300)).toBe("5 minutes");
  });
});

describe("labelForUseCase", () => {
  it("falls back to the identifier for a use case this build does not know", () => {
    expect(labelForUseCase("oauth-session-tokens")).toBe(
      "OAuth session tokens",
    );
    expect(labelForUseCase("future-use-case")).toBe("future-use-case");
  });
});
