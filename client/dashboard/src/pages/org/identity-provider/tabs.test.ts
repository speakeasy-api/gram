import { describe, expect, it } from "vitest";
import { enterpriseManagedAuthHref, OKTA_VIEWS, oktaViewHref } from "./tabs";

describe("identity links", () => {
  it("links to the EMA landing", () => {
    expect(enterpriseManagedAuthHref()).toBe("?tab=enterprise-managed-auth");
  });

  it.each(OKTA_VIEWS)("links to the Okta %s view with a section", (view) => {
    expect(oktaViewHref(view)).toBe(
      `?tab=enterprise-managed-auth&provider=okta&view=${view}`,
    );
    expect(oktaViewHref(view, "section")).toBe(
      `?tab=enterprise-managed-auth&provider=okta&view=${view}#section`,
    );
  });
});
