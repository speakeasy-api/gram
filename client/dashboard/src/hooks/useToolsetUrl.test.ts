import { describe, expect, it } from "vitest";
import { platformEndpointSlug } from "./useToolsetUrl";

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
