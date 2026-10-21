import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { DEMO_ORG_SLUG } from "@/lib/demo";
import { useSensitiveContentRestricted } from "./useSensitiveContentRestricted";

const session = vi.hoisted(() => ({
  organization: { slug: "local-workspace" },
  organizationOverride: false,
  impersonatorEmail: undefined as string | undefined,
}));
vi.mock("@/contexts/Auth", () => ({ useSession: () => session }));
beforeEach(() => {
  session.organization.slug = "local-workspace";
  session.organizationOverride = false;
  session.impersonatorEmail = undefined;
});

describe("useSensitiveContentRestricted", () => {
  it("leaves ordinary sessions unrestricted", () => {
    expect(
      renderHook(() => useSensitiveContentRestricted()).result.current,
    ).toBe(false);
  });
  it.each(["support", "workos"])(
    "protects content for %s impersonation",
    (mode) => {
      session.organizationOverride = mode === "support";
      session.impersonatorEmail =
        mode === "workos" ? "support@example.test" : undefined;
      expect(
        renderHook(() => useSensitiveContentRestricted()).result.current,
      ).toBe(true);
    },
  );
  it.each(["support", "workos"])(
    "preserves demo access for %s impersonation",
    (mode) => {
      session.organization.slug = DEMO_ORG_SLUG;
      session.organizationOverride = mode === "support";
      session.impersonatorEmail =
        mode === "workos" ? "support@example.test" : undefined;
      expect(
        renderHook(() => useSensitiveContentRestricted()).result.current,
      ).toBe(false);
    },
  );
  it("reacts when an ordinary session enters support access", () => {
    const hook = renderHook(() => useSensitiveContentRestricted());
    expect(hook.result.current).toBe(false);
    session.organizationOverride = true;
    hook.rerender();
    expect(hook.result.current).toBe(true);
  });
});
