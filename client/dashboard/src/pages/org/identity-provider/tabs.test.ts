import { describe, expect, it } from "vitest";
import { identityProvidersHref, OKTA_VIEWS, oktaViewHref } from "./tabs";

describe("identity links", () => {
  it("links to the EMA landing", () => {
    expect(identityProvidersHref()).toBe("?tab=identity-providers");
  });

  it.each(OKTA_VIEWS)("links to the Okta %s view with a section", (view) => {
    expect(oktaViewHref(view)).toBe(
      `?tab=identity-providers&provider=okta&view=${view}`,
    );
    expect(oktaViewHref(view, "section")).toBe(
      `?tab=identity-providers&provider=okta&view=${view}#section`,
    );
  });
});
