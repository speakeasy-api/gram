import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { DEMO_ORG_SLUG } from "@/lib/demo";
import { useKillswitchAccess } from "./useKillswitchAccess";

const mocks = vi.hoisted(() => ({
  session: {
    organization: { slug: "customer-org" },
    organizationOverride: false,
    impersonatorEmail: undefined as string | undefined,
  },
  rbac: { isLoading: false, hasScope: vi.fn(() => true) },
  featureFlags: {
    status: "ready" as "loading" | "ready" | "error",
    revision: 0,
  },
  isFeatureEnabled: vi.fn(() => true as boolean | undefined),
}));

vi.mock("@/contexts/Auth", () => ({ useSession: () => mocks.session }));
vi.mock("@/hooks/useRBAC", () => ({ useRBAC: () => mocks.rbac }));
vi.mock("@/contexts/Telemetry", () => ({
  useTelemetryContext: () => ({
    telemetry: { isFeatureEnabled: mocks.isFeatureEnabled },
    featureFlags: mocks.featureFlags,
  }),
}));

beforeEach(() => {
  mocks.session.organization.slug = "customer-org";
  mocks.session.organizationOverride = false;
  mocks.session.impersonatorEmail = undefined;
  mocks.rbac.isLoading = false;
  mocks.rbac.hasScope.mockReturnValue(true);
  mocks.featureFlags.status = "ready";
  mocks.isFeatureEnabled.mockReturnValue(true);
});

describe("useKillswitchAccess", () => {
  it.each(["rbac", "rollout"] as const)(
    "waits without granting access while %s state loads",
    (loadingState) => {
      mocks.rbac.isLoading = loadingState === "rbac";
      mocks.featureFlags.status =
        loadingState === "rollout" ? "loading" : "ready";
      expect(renderHook(() => useKillswitchAccess()).result.current).toEqual({
        canAccess: false,
        isLoading: true,
        reason: "loading",
      });
    },
  );

  it.each(["error", "ready"] as const)(
    "fails hidden when rollout state is %s and the flag is unavailable",
    (status) => {
      mocks.featureFlags.status = status;
      mocks.isFeatureEnabled.mockReturnValue(undefined);
      expect(
        renderHook(() => useKillswitchAccess()).result.current.reason,
      ).toBe("rollout");
    },
  );

  it("gates the shared demo before loading access state", () => {
    mocks.session.organization.slug = DEMO_ORG_SLUG;
    mocks.featureFlags.status = "loading";
    mocks.rbac.isLoading = true;
    expect(renderHook(() => useKillswitchAccess()).result.current).toEqual({
      canAccess: false,
      isLoading: false,
      reason: "demo",
    });
  });

  it("rejects support and impersonation sessions", () => {
    mocks.session.organizationOverride = true;
    expect(renderHook(() => useKillswitchAccess()).result.current.reason).toBe(
      "support",
    );
    mocks.session.organizationOverride = false;
    mocks.session.impersonatorEmail = "support@example.test";
    expect(renderHook(() => useKillswitchAccess()).result.current.reason).toBe(
      "support",
    );
  });

  it("requires org:admin", () => {
    mocks.rbac.hasScope.mockReturnValue(false);
    const result = renderHook(() => useKillswitchAccess()).result.current;
    expect(mocks.rbac.hasScope).toHaveBeenCalledWith("org:admin");
    expect(result.reason).toBe("scope");
  });

  it("allows customer admins in the rollout", () => {
    expect(renderHook(() => useKillswitchAccess()).result.current).toEqual({
      canAccess: true,
      isLoading: false,
      reason: "allowed",
    });
  });
});
