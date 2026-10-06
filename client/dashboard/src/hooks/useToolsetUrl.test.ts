import { describe, expect, it, vi } from "vitest";
import { gatewayInstallPageUrl, platformEndpointSlug } from "./useToolsetUrl";

vi.mock("@/lib/utils", () => ({
  getServerURL: () => "https://gram.example",
}));

describe("platformEndpointSlug", () => {
  it("skips custom-domain endpoints", () => {
    expect(
      platformEndpointSlug([
        { slug: "on-custom-domain", customDomainId: "domain-1" },
        { slug: "on-gram", customDomainId: undefined },
      ]),
    ).toBe("on-gram");
  });

  it("returns undefined when every endpoint is on a custom domain", () => {
    expect(
      platformEndpointSlug([
        { slug: "on-custom-domain", customDomainId: "domain-1" },
      ]),
    ).toBeUndefined();
  });
});

describe("gatewayInstallPageUrl", () => {
  it("links a platform endpoint on the Gram origin", () => {
    expect(
      gatewayInstallPageUrl([
        { slug: "on-custom-domain", customDomainId: "domain-1" },
        { slug: "on-gram", customDomainId: undefined },
      ]),
    ).toBe("https://gram.example/mcp/on-gram/install");
  });

  it("marks a custom-domain-only slug so the Gram origin can resolve it", () => {
    expect(
      gatewayInstallPageUrl([
        { slug: "on-custom-domain", customDomainId: "domain-1" },
      ]),
    ).toBe("https://gram.example/mcp/on-custom-domain/install?domain=custom");
  });

  it("returns undefined without a slug", () => {
    expect(gatewayInstallPageUrl([])).toBeUndefined();
  });
});
