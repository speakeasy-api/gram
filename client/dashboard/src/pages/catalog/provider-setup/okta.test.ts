// @vitest-environment node
import { describe, expect, it } from "vitest";
import { oktaConnection, oktaOrg, oktaSelectionForUrl } from "./okta";
import { initialState, reduce, ready, validate } from "./state";

describe("native Okta setup", () => {
  it("continues past grant instructions without collecting scopes", () => {
    const s = initialState("okta");
    s.step = "permissions";
    expect(validate(s)).toEqual({});
    expect(reduce(s, { type: "next" }).step).toBe("credentials");
  });
  it("resolves the org into the actual upstream and org authorization server", () => {
    expect(oktaOrg("https://EXAMPLE.okta.com/")).toBe(
      "https://example.okta.com",
    );
    expect(
      oktaConnection({ oktaOrg: "example.okta.com", oktaAppType: "web" }),
    ).toMatchObject({
      url: "https://example.okta.com/mcp",
      issuer: "https://example.okta.com",
      authorizationEndpoint: "https://example.okta.com/oauth2/v1/authorize",
      tokenEndpoint: "https://example.okta.com/oauth2/v1/token",
      authMethod: "client_secret_basic",
    });
    expect(
      oktaConnection({ oktaOrg: "example.okta.com", oktaAppType: "native" })
        ?.authMethod,
    ).toBe("none");
    expect(oktaSelectionForUrl("https://example.okta.com/mcp")).toEqual({
      oktaOrg: "https://example.okta.com",
    });
    expect(oktaSelectionForUrl("https://example.okta.com/other")).toBeNull();
  });
  it.each([
    "http://example.okta.com",
    "https://example.okta.com/oauth2/default",
    "https://user@example.okta.com",
    "example.okta.com:8443",
    "example.okta.com?x=1",
    "example.okta.com.evil.test",
    "example.okta-gov.com",
    "localhost",
    "127.0.0.1",
  ])("rejects unsupported org %s", (value) => {
    expect(oktaOrg(value)).toBeNull();
  });
  it("requires an Okta organization to complete an otherwise connected setup", () => {
    const s = initialState("okta");
    Object.assign(s, { saved: true, authorized: true, tested: true });
    s.values.oktaOrg = "example.okta.com";
    expect(reduce(s, { type: "complete" }).screen).toBe("success");

    s.values.oktaOrg = "";
    expect(ready(s)).toBe(false);
    expect(reduce(s, { type: "complete" }).screen).toBe("setup");
  });
  it("requires a secret only for Web apps and invalidates changed configuration", () => {
    let s = initialState("okta");
    s.step = "credentials";
    Object.assign(s.values, {
      oktaOrg: "example.okta.com",
      clientId: "example-client",
    });
    expect(validate(s).clientSecret).toBeTruthy();
    s.values.oktaAppType = "native";
    expect(validate(s)).toEqual({});
    Object.assign(s, { saved: true, authorized: true, tested: true });
    expect(ready(s)).toBe(true);
    s = reduce(s, {
      type: "field",
      field: "oktaOrg",
      value: "other.okta.com",
    });
    expect(s).toMatchObject({
      saved: false,
      authorized: false,
      tested: false,
      configChanged: true,
    });
    expect(s.values.clientId).toBe("");
    expect(reduce(s, { type: "complete" }).screen).toBe("setup");
  });
});

it("routes only native Okta endpoints into setup", async () => {
  const { setupProviderForUrl } =
    await import("../../mcp/x/setup/provider-setup");
  expect(setupProviderForUrl("https://example.okta.com/mcp")).toBe("okta");
  expect(setupProviderForUrl("https://example.okta.com/not-mcp")).toBeNull();
});

it("shows enablement instructions after Setup Plan and continues using the standard action", () => {
  const enabled = reduce(initialState("okta"), { type: "next" });
  expect(enabled.step).toBe("enable");
  expect(reduce(enabled, { type: "next" }).step).toBe("organization");
  expect(enabled.saved).toBe(false);
  expect(enabled.authorized).toBe(false);
  expect(enabled.tested).toBe(false);
});
