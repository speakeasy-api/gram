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
  scopeOverrideHeader: null as string | null,
}));

vi.mock("@/components/dev-toolbar-utils", () => ({
  getRBACScopeOverrideHeader: () => mocks.scopeOverrideHeader,
}));
vi.mock("@/contexts/Auth", () => ({
  useIsPlatformAdmin: () => false,
  useSession: () => mocks.session,
}));
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
  mocks.isFeatureEnabled.mockReset().mockReturnValue(true);
  mocks.scopeOverrideHeader = null;
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

  it("fails hidden when loading rollout state fails", () => {
    mocks.featureFlags.status = "error";
    expect(renderHook(() => useKillswitchAccess()).result.current.reason).toBe(
      "rollout",
    );
    expect(mocks.isFeatureEnabled).not.toHaveBeenCalled();
  });

  it("fails hidden when rollout is ready but the flag is unavailable", () => {
    mocks.isFeatureEnabled.mockReturnValue(undefined);
    expect(renderHook(() => useKillswitchAccess()).result.current.reason).toBe(
      "rollout",
    );
  });

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

  it("denies access while the SDK sends a scope override", () => {
    mocks.scopeOverrideHeader = "org:admin";
    expect(renderHook(() => useKillswitchAccess()).result.current).toEqual({
      canAccess: false,
      isLoading: false,
      reason: "override",
    });
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
