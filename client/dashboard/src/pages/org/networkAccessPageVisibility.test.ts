import { describe, expect, it } from "vitest";

import { shouldShowNetworkAccessPage } from "./networkAccessPageVisibility";

describe("shouldShowNetworkAccessPage", () => {
  it.each(["loading", "enabled", "error"] as const)(
    "shows Network Access to every viewer when entitlement is %s",
    (status) => {
      expect(shouldShowNetworkAccessPage(status, false)).toBe(true);
    },
  );

  it("shows Network Access to admins even before staff enables Tailscale", () => {
    expect(shouldShowNetworkAccessPage("disabled", true)).toBe(true);
  });

  it("keeps Custom Domain for non-admins without private access", () => {
    expect(shouldShowNetworkAccessPage("disabled", false)).toBe(false);
  });
});
