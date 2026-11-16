// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  identityProvidersHref,
  OKTA_VIEWS,
  oktaViewHref,
  speakeasyProviderHref,
} from "./tabs";

describe("identity links", () => {
  it("links to the EMA landing", () => {
    expect(identityProvidersHref()).toBe("?tab=identity-providers");
  });

  it("links to the Speakeasy provider page", () => {
    expect(speakeasyProviderHref()).toBe(
      "?tab=identity-providers&provider=speakeasy",
    );
  });

  it.each(OKTA_VIEWS)("links to the Okta %s view with a section", (view) => {
    expect(oktaViewHref(view)).toBe(
      `?tab=identity-providers&provider=okta&view=${view}`,
    );
    expect(oktaViewHref(view, "section")).toBe(
      `?tab=identity-providers&provider=okta&view=${view}#section`,
    );
  });

  it("carries an EMA setup in progress", () => {
    const href = new URLSearchParams(
      oktaViewHref("setup", undefined, {
        providerName: "acme-workforce",
        path: "/org/x?tab=y",
      }),
    );
    expect(href.get("view")).toBe("setup");
    expect(href.get("emaFor")).toBe("acme-workforce");
    expect(href.get("emaReturn")).toBe("/org/x?tab=y");
  });
});
