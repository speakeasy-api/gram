import { describe, expect, it } from "vitest";

import { shouldShowNetworkAccessPage } from "./networkAccessPageVisibility";

describe("shouldShowNetworkAccessPage", () => {
  it.each(["loading", "enabled", "error"] as const)(
    "shows Network Access to every viewer when entitlement is %s",
    (status) => {
      expect(shouldShowNetworkAccessPage(status, false, false)).toBe(true);
    },
  );

  it("shows Network Access when an admin may need ingress recovery", () => {
    expect(shouldShowNetworkAccessPage("disabled", true, true)).toBe(true);
  });

  it("shows Custom Domain only after confirming no ingress recovery path", () => {
    expect(shouldShowNetworkAccessPage("disabled", false, true)).toBe(false);
    expect(shouldShowNetworkAccessPage("disabled", true, false)).toBe(false);
  });
});
