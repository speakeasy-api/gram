import { describe, expect, it } from "vitest";
import { oktaIssuerUrl } from "./oktaEma";

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
