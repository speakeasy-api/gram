import { describe, expect, it, vi } from "vitest";

import type { FeatureFlagResult } from "@/hooks/useFeatureFlag";

import { agentIdentityRolloutReason } from "./agent-management-availability";

vi.mock("@/contexts/Auth", () => ({
  useIsPlatformAdmin: () => false,
  useOrganization: () => ({ slug: "org" }),
  useSession: () => ({}),
}));

const enabled: FeatureFlagResult = { status: "enabled" };
const disabled: FeatureFlagResult = { status: "disabled" };
const loading: FeatureFlagResult = { status: "loading" };
const missing: FeatureFlagResult = { status: "missing" };

describe("agent identity rollout", () => {
  it("is available only when both agent management and credentials are on", () => {
    expect(agentIdentityRolloutReason(enabled, enabled)).toBeNull();
  });

  it("is unavailable when either flag is off", () => {
    expect(agentIdentityRolloutReason(disabled, enabled)).toMatch(/disabled/);
    expect(agentIdentityRolloutReason(enabled, disabled)).toMatch(/disabled/);
  });

  it("reports a disabled flag over one still loading", () => {
    expect(agentIdentityRolloutReason(loading, disabled)).toMatch(/disabled/);
  });

  it("waits while a flag is loading", () => {
    expect(agentIdentityRolloutReason(enabled, loading)).toMatch(/Checking/);
  });

  it("fails closed when a flag cannot be read", () => {
    expect(agentIdentityRolloutReason(missing, enabled)).toMatch(
      /could not be determined/,
    );
  });
});
