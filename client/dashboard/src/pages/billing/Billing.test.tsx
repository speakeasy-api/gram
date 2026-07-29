import { cleanup, render, screen } from "@testing-library/react";
import type { TierLimits } from "@gram/client/models/components/tierlimits.js";
import type { UsageTiers } from "@gram/client/models/components/usagetiers.js";
import { afterEach, describe, expect, it } from "vitest";
import {
  getChatCreditEntitlement,
  TierIncludedItems,
} from "./billing-entitlements";

afterEach(cleanup);

function tierLimits(includedCredits: number): TierLimits {
  return {
    basePrice: 29,
    includedToolCalls: 10_000,
    includedServers: 3,
    includedCredits,
    pricePerAdditionalToolCall: 0,
    pricePerAdditionalServer: 0,
    featureBullets: [],
    includedBullets: [],
    addOnBullets: [],
  };
}

describe("Billing chat-credit entitlement", () => {
  it("uses the Pro pricing configuration for the Pro usage entitlement", () => {
    const usageTiers: UsageTiers = {
      free: tierLimits(5),
      pro: tierLimits(25),
      enterprise: tierLimits(0),
    };

    expect(getChatCreditEntitlement("__deprecated__pro", usageTiers)).toBe(
      usageTiers.pro.includedCredits,
    );
  });

  it("renders the pricing entitlement with the same explicit unit", () => {
    render(<TierIncludedItems tierLimits={tierLimits(25)} />);

    expect(screen.getByText("25 chat credits / month")).toBeTruthy();
  });
});
