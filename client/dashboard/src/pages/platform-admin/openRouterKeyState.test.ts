import { describe, expect, it } from "vitest";
import { causeLabels, keyAction } from "./openRouterKeyState";

describe("OpenRouter key state", () => {
  it("renders every known and unknown disable cause", () => {
    expect(causeLabels(["admin_lock", "trial_demotion"])).toEqual([
      "Admin lock",
      "Trial demotion",
    ]);
    expect(causeLabels(["billing_inactive", "future_cause"])).toEqual([
      "Billing inactive",
      "future_cause",
    ]);
  });

  it("bases the admin action only on the admin lock cause", () => {
    expect(keyAction([])).toBe("disable");
    expect(keyAction(["trial_demotion"])).toBe("disable");
    expect(keyAction(["admin_lock"])).toBe("remove-admin-lock");
    expect(keyAction(["admin_lock", "billing_inactive"])).toBe(
      "remove-admin-lock",
    );
  });
});
