import { expect, it } from "vitest";
import { showPaymentMethods } from "./payment-method-visibility";

it("hides payment methods for enterprise organizations outside an active trial", () => {
  expect(showPaymentMethods("enterprise", "none", false)).toBe(false);
  expect(showPaymentMethods("enterprise", "none", true)).toBe(false);
});

it("preserves the eligible active-trial exception", () => {
  expect(showPaymentMethods("enterprise", "active", true)).toBe(true);
  expect(showPaymentMethods("enterprise", "active", false)).toBe(false);
});

it("shows PAYG management while preserving the active-trial eligibility gate", () => {
  expect(showPaymentMethods("payg", "none", false)).toBe(true);
  expect(showPaymentMethods("payg", "active", false)).toBe(false);
  expect(showPaymentMethods("payg", "active", true)).toBe(true);
});

it("does not expose Stripe management for legacy plans", () => {
  expect(showPaymentMethods("base_PAID", "none", false)).toBe(false);
  expect(showPaymentMethods("__deprecated__pro", "none", false)).toBe(false);
});
