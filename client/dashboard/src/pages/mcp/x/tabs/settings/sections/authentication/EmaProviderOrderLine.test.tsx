import type { EMAProviderOrder } from "@gram/client/models/components/emaproviderorder.js";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { EmaProviderOrderLine } from "./EmaProviderOrderLine";

vi.mock("@/lib/remote-identity", () => ({
  useRemoteSessionIssuersByIds: () => ({
    items: [{ id: "okta", name: "Okta" }],
    isLoading: false,
    isError: false,
  }),
}));

afterEach(cleanup);

const line = () => screen.getByTestId("ema-provider-order").textContent;

describe("EmaProviderOrderLine", () => {
  it("lists a saved order by name, marking providers this server doesn't use", () => {
    const order: EMAProviderOrder = {
      isDefault: false,
      vendorIssuer: "https://mcp.vendor.example.com",
      providers: [
        {
          kind: "external",
          remoteSessionIssuerId: "okta",
          unavailableReason: "not_trusted_by_endpoint",
        },
        { kind: "speakeasy", internalSessionIssuerId: "internal" },
      ],
    };
    render(<EmaProviderOrderLine order={order} speakeasyOnly />);
    expect(line()).toBe(
      "Tries: Okta (not used by this server) → Speakeasy · applies to every server on mcp.vendor.example.com",
    );
  });

  it("says no order is saved yet on a Speakeasy-only server", () => {
    const order: EMAProviderOrder = {
      isDefault: true,
      vendorIssuer: "https://mcp.vendor.example.com",
      providers: [{ kind: "external" }],
    };
    render(<EmaProviderOrderLine order={order} speakeasyOnly />);
    expect(line()).toBe(
      "Provider order: no order saved yet for mcp.vendor.example.com.",
    );
  });

  it("names the default external provider on a server that trusts one", () => {
    const order: EMAProviderOrder = {
      isDefault: true,
      vendorIssuer: "https://mcp.vendor.example.com",
      providers: [{ kind: "external", remoteSessionIssuerId: "okta" }],
    };
    render(<EmaProviderOrderLine order={order} speakeasyOnly={false} />);
    expect(line()).toBe(
      "Tries: Okta (default) · applies to every server on mcp.vendor.example.com",
    );
  });
});
