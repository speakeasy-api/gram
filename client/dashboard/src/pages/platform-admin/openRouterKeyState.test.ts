import { adminOpenRouterKeyFromJSON } from "@gram/client/models/components/adminopenrouterkey.js";
import { describe, expect, it } from "vitest";
import {
  causeLabels,
  effectiveDisabled,
  keyAction,
} from "./openRouterKeyState";

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
    expect(causeLabels(["toString", "constructor", "__proto__"])).toEqual([
      "toString",
      "constructor",
      "__proto__",
    ]);
    expect(causeLabels(undefined)).toEqual([]);
  });

  it("preserves an unknown cause through SDK response decoding", () => {
    const result = adminOpenRouterKeyFromJSON(
      JSON.stringify({
        created_at: "2026-01-01T00:00:00Z",
        disable_causes: ["future_cause"],
        disabled: true,
        gram_account_type: "pro",
        key_type: "chat",
        monthly_credits: 10,
        organization_id: "organization-id",
        organization_name: "Example organization",
        organization_slug: "example-organization",
        updated_at: "2026-01-01T00:00:00Z",
      }),
    );

    expect(result.ok).toBe(true);
    if (!result.ok) throw result.error;
    expect(causeLabels(result.value.disableCauses)).toEqual(["future_cause"]);
  });

  it("uses writable disabled only for legacy unclassified rows", () => {
    expect(effectiveDisabled({ disabled: true })).toBe(true);
    expect(effectiveDisabled({ disabled: false })).toBe(false);
    expect(effectiveDisabled({ disabled: true, disableCauses: [] })).toBe(
      false,
    );
    expect(
      effectiveDisabled({ disabled: false, disableCauses: ["future_cause"] }),
    ).toBe(true);
  });

  it("bases the admin action only on the admin lock cause", () => {
    expect(keyAction(undefined)).toBeNull();
    expect(keyAction(null)).toBeNull();
    expect(keyAction([])).toBe("disable");
    expect(keyAction(["trial_demotion"])).toBe("disable");
    expect(keyAction(["future_cause"])).toBe("disable");
    expect(keyAction(["admin_lock"])).toBe("remove-admin-lock");
    expect(keyAction(["admin_lock", "billing_inactive"])).toBe(
      "remove-admin-lock",
    );
  });
});
