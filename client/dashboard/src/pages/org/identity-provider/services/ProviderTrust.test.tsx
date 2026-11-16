import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { ProviderTrust } from "./ProviderTrust";
import { providerView, type ProviderView } from "./emaServiceView";
import { unrecognized } from "@gram/client/types/unrecognized.js";

const provider: ProviderView = {
  key: "speakeasy",
  kind: "speakeasy",
  name: "Speakeasy",
  placeholder: false,
  usable: true,
  unavailable: "",
  unusedBySignIn: false,
};

function tone(): string | null {
  return (
    document.querySelector("[data-tone]")?.getAttribute("data-tone") ?? null
  );
}

afterEach(cleanup);

describe("ProviderTrust", () => {
  it("shows unknown availability neutrally even after past acceptance", () => {
    render(
      <ProviderTrust
        provider={providerView(
          {
            kind: "speakeasy",
            unavailableReason: unrecognized("future_reason"),
            lastAcceptedAt: new Date(),
          },
          {
            issuer: undefined,
            speakeasyIssuerUrl: "https://example.com/issuer",
          },
        )}
      />,
    );
    expect(tone()).toBe("neutral");
    expect(screen.getByText(/Unknown availability/)).toBeTruthy();
  });
  it("is green once the service accepted a usable provider", () => {
    render(
      <ProviderTrust provider={{ ...provider, lastAcceptedAt: new Date() }} />,
    );
    expect(tone()).toBe("success");
  });

  it("warns about a provider that can't serve, even after past acceptance", () => {
    render(
      <ProviderTrust
        provider={{
          ...provider,
          usable: false,
          unavailable: "Signing is paused.",
          lastAcceptedAt: new Date(),
        }}
      />,
    );
    expect(tone()).toBe("warning");
    expect(screen.getByText("Signing is paused.")).toBeTruthy();
  });
});
