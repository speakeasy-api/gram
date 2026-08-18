import { renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { emptySession, SessionContext } from "@/contexts/Auth";
import { ProductTier, useProductTier } from "./useProductTier";

const renderTier = (session: {
  rawGramAccountType: string;
  hasActiveSubscription: boolean;
}): ProductTier => {
  const { result } = renderHook(() => useProductTier(), {
    wrapper: ({ children }) => (
      <SessionContext.Provider value={{ ...emptySession, ...session }}>
        {children}
      </SessionContext.Provider>
    ),
  });

  return result.current;
};

describe("useProductTier", () => {
  it("resolves a payg account type before the subscription fallback", () => {
    expect(
      renderTier({ rawGramAccountType: "payg", hasActiveSubscription: false }),
    ).toBe("payg");
    expect(
      renderTier({ rawGramAccountType: "payg", hasActiveSubscription: true }),
    ).toBe("payg");
  });

  it("resolves the other raw account types", () => {
    expect(
      renderTier({
        rawGramAccountType: "enterprise",
        hasActiveSubscription: false,
      }),
    ).toBe("enterprise");
    expect(
      renderTier({ rawGramAccountType: "pro", hasActiveSubscription: false }),
    ).toBe("__deprecated__pro");
  });

  it("falls back to the subscription state for unrecognized account types", () => {
    expect(
      renderTier({ rawGramAccountType: "free", hasActiveSubscription: true }),
    ).toBe("base_PAID");
    expect(
      renderTier({ rawGramAccountType: "free", hasActiveSubscription: false }),
    ).toBe("base");
  });
});
