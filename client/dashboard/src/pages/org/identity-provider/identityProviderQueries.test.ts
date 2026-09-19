import { describe, expect, it } from "vitest";
import {
  enterpriseManagedAuthHref,
  identityTabHref,
  legacyIdentityProviderSearch,
} from "./identityProviderQueries";

describe("identity links", () => {
  it("links to the EMA landing and encodes provider and view query values", () => {
    expect(enterpriseManagedAuthHref()).toBe("?tab=enterprise-managed-auth");
    expect(enterpriseManagedAuthHref("a&b", "space here", "connection")).toBe(
      "?tab=enterprise-managed-auth&provider=a%26b&view=space+here#connection",
    );
    expect(enterpriseManagedAuthHref("okta")).toBe(
      "?tab=enterprise-managed-auth&provider=okta",
    );
  });

  it.each([
    ["provider", "setup"],
    ["applications", "applications"],
    ["cross-app-access", "cross-app-access"],
  ] as const)("maps %s to the Okta %s workspace", (tab, view) => {
    expect(identityTabHref(tab, "section")).toBe(
      `?tab=enterprise-managed-auth&provider=okta&view=${view}#section`,
    );
  });

  it("keeps employee SSO links separate", () => {
    expect(identityTabHref("sso")).toBe("?tab=sso");
    expect(identityTabHref("sso", "directory_sync")).toBe(
      "?tab=sso#directory_sync",
    );
  });

  it("preserves repeated unrelated parameters without mutating the legacy search", () => {
    const search = new URLSearchParams(
      "tab=okta&okta=applications&filter=a&filter=b&provider=other&view=old",
    );
    const result = new URLSearchParams(
      legacyIdentityProviderSearch(search, "applications"),
    );
    expect(result.getAll("filter")).toEqual(["a", "b"]);
    expect(result.get("tab")).toBe("enterprise-managed-auth");
    expect(result.get("provider")).toBe("okta");
    expect(result.get("view")).toBe("applications");
    expect(result.has("okta")).toBe(false);
    expect(search.get("tab")).toBe("okta");
  });
});
