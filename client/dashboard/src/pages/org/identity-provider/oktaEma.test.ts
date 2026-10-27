import { describe, expect, it } from "vitest";
import { emaReturnHref, oktaIssuerUrl, parseEmaReturn } from "./oktaEma";

describe("oktaIssuerUrl", () => {
  it.each([
    ["acme.okta.com", "https://acme.okta.com"],
    ["https://acme.okta.com/", "https://acme.okta.com"],
    ["ACME.okta.com/app/UserHome", "https://acme.okta.com"],
    ["https://acme-admin.okta.com/admin/dashboard", "https://acme.okta.com"],
    ["https://acme-admin.oktapreview.com", "https://acme.oktapreview.com"],
    ["login.example.com", "https://login.example.com"],
  ])("reads %s as %s", (domain, issuer) => {
    expect(oktaIssuerUrl(domain)).toBe(issuer);
  });

  it.each(["", "   ", "acme", "http://acme.okta.com", "not a url"])(
    "refuses %j",
    (domain) => {
      expect(oktaIssuerUrl(domain)).toBeUndefined();
    },
  );
});

describe("parseEmaReturn", () => {
  it("keeps a same-site path", () => {
    expect(parseEmaReturn("acme-workforce", "/org/projects/p/x")).toEqual({
      providerName: "acme-workforce",
      path: "/org/projects/p/x",
    });
  });

  it.each(["https://evil.example", "//evil.example", "/\\evil.example", "x"])(
    "refuses %s",
    (path) => {
      expect(parseEmaReturn("acme-workforce", path)).toBeUndefined();
    },
  );

  it("needs both the name and the path", () => {
    expect(parseEmaReturn(null, "/x")).toBeUndefined();
    expect(parseEmaReturn("acme-workforce", null)).toBeUndefined();
  });
});

describe("emaReturnHref", () => {
  it("asks the provider page to turn EMA on with the issuer", () => {
    expect(
      emaReturnHref(
        { providerName: "acme-workforce", path: "/org/x" },
        "https://acme.okta.com",
      ),
    ).toBe("/org/x?ema=https%3A%2F%2Facme.okta.com");
  });
});
