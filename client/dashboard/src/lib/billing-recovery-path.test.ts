import { expect, it } from "vitest";
import { isBillingRecoveryPath } from "./billing-recovery-path";
it("only exempts exact billing recovery routes for the active organization", () => {
  for (const path of [
    "/test-org/billing/setup",
    "/test-org/billing/payment-method",
  ])
    expect(isBillingRecoveryPath(path, "", "test-org")).toBe(true);
  for (const path of [
    "/test-org/billing",
    "/other-org/billing/setup",
    "/test-org/billing/setup/extra",
    "/test-org/team",
  ])
    expect(isBillingRecoveryPath(path, "", "test-org")).toBe(false);
  expect(
    isBillingRecoveryPath(
      "/test-org/billing",
      "?session_id=cs_test_example",
      "test-org",
    ),
  ).toBe(true);
  expect(
    isBillingRecoveryPath(
      "/other-org/billing",
      "?session_id=cs_test_example",
      "test-org",
    ),
  ).toBe(false);
  expect(isBillingRecoveryPath("/test-org/billing/setup", "", undefined)).toBe(
    false,
  );
});
