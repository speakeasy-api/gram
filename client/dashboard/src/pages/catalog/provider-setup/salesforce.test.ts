import { describe, expect, it } from "vitest";
import { initialState, reduce, ready, steps, validate } from "./state";
import {
  salesforceConnection,
  salesforceSelectionForUrl,
  salesforceInstance,
} from "./salesforce";
import {
  setupProviderForUrl,
  loadProviderProgress,
  saveProviderProgress,
} from "@/pages/mcp/x/setup/provider-setup";

describe("Salesforce connection contracts", () => {
  it("maps native environments and server choices to their actual upstream", () => {
    const s = initialState("salesforce");
    expect(salesforceConnection(s.values)).toMatchObject({
      url: "https://api.salesforce.com/platform/mcp/v1/platform/sobject-reads",
      issuer: "https://login.salesforce.com",
      scopes: ["mcp_api", "refresh_token"],
      authMethod: "none",
    });
    s.values.environment = "sandbox";
    s.values.salesforceServer = "sobject-mutations";
    expect(salesforceConnection(s.values)).toMatchObject({
      url: "https://api.salesforce.com/platform/mcp/v1/sandbox/platform/sobject-mutations",
      authorizationEndpoint:
        "https://test.salesforce.com/services/oauth2/authorize",
      tokenEndpoint: "https://test.salesforce.com/services/oauth2/token",
    });
  });
  it("keeps the hosted endpoint fixed and configures the instance separately", () => {
    const s = initialState("salesforce");
    s.values.integration = "hosted";
    s.values.instanceUrl = " https://example.my.salesforce.com/ ";
    s.values.clientSecret = "secret";
    expect(salesforceConnection(s.values)).toMatchObject({
      url: "https://app.getgram.ai/mcp/speakeasy-team-salesforce",
      instanceUrl: "https://example.my.salesforce.com",
      authorizationEndpoint:
        "https://example.my.salesforce.com/services/oauth2/authorize",
      scopes: ["api", "refresh_token"],
      authMethod: "client_secret_post",
    });
    s.values.environment = "sandbox";
    expect(salesforceConnection(s.values)?.url).toBe(
      "https://app.getgram.ai/mcp/speakeasy-team-salesforce",
    );
  });
  it.each([
    "http://example.my.salesforce.com",
    "https://salesforce.com.evil.test",
    "https://example.my.salesforce.com/path",
    "https://u:p@example.my.salesforce.com",
    "https://example.my.salesforce.com?token=x",
    "https://example.my.salesforce.com:8443",
  ])("rejects unsafe instance URLs: %s", (url) => {
    expect(salesforceInstance(url)).toBeNull();
  });
  it("recognizes both exact integrations without matching arbitrary hosted servers", () => {
    for (const url of [
      "https://api.salesforce.com/platform/mcp/v1/platform/sobject-all",
      "https://api.salesforce.com/platform/mcp/v1/sandbox/platform/sobject-deletes",
      "https://app.getgram.ai/mcp/speakeasy-team-salesforce",
    ])
      expect(setupProviderForUrl(url)).toBe("salesforce");
    for (const url of [
      "https://api.salesforce.com.evil.test/platform/mcp/v1/platform/sobject-reads",
      "https://app.getgram.ai/mcp/other-salesforce",
      "https://api.salesforce.com/platform/mcp/v1/platform/unknown",
      "https://app.getgram.ai/mcp/speakeasy-team-salesforce?x=1",
    ])
      expect(setupProviderForUrl(url)).toBeNull();
    expect(
      salesforceSelectionForUrl(
        "https://api.salesforce.com/platform/mcp/v1/sandbox/platform/sobject-all",
      ),
    ).toMatchObject({
      environment: "sandbox",
      salesforceServer: "sobject-all",
      integration: "native",
    });
  });
  it("accepts the Consumer Key-only configuration and optional hosted secret", () => {
    let s = { ...initialState("salesforce"), step: "credentials" };
    s.values.clientId = "key";
    expect(validate(s)).toEqual({});
    s.values.integration = "hosted";
    s.values.instanceUrl = "https://example.my.salesforce.com";
    expect(validate(s)).toEqual({});
    s.values.clientSecret = "secret";
    expect(validate(s)).toEqual({});
  });
  it("invalidates app credentials and verification when the connection changes", () => {
    for (const [field, value] of [
      ["integration", "hosted"],
      ["environment", "sandbox"],
      ["salesforceServer", "sobject-all"],
      ["instanceUrl", "https://other.my.salesforce.com"],
      ["path", "support"],
    ] as const) {
      const s = initialState("salesforce");
      s.values.clientId = "key";
      s.values.clientSecret = "secret";
      const changed = reduce(
        { ...s, saved: true, authorized: true, tested: true, step: "test" },
        { type: "field", field, value },
      );
      expect([changed.saved, changed.authorized, changed.tested]).toEqual([
        false,
        false,
        false,
      ]);
      expect(changed.values.clientId).toBe("");
      expect(changed.values.clientSecret).toBe("");
    }
  });
  it("keeps support unresolved and completion gated on the real test", () => {
    const s = initialState("salesforce");
    expect(steps(s).map((x) => x.id)).toContain("activation");
    s.values.path = "support";
    expect(steps(s).map((x) => x.id)).toContain("support");
    expect(ready({ ...s, saved: true, authorized: true, tested: true })).toBe(
      false,
    );
    expect(
      reduce(
        { ...s, saved: true, authorized: true, tested: true },
        { type: "complete" },
      ).screen,
    ).toBe("setup");
    s.values.path = "own";
    expect(
      reduce(
        { ...s, saved: true, authorized: true, tested: false },
        { type: "complete" },
      ).screen,
    ).toBe("setup");
  });
  it("restores non-secret choices but never restores authorization or test success", () => {
    const s = initialState("salesforce");
    Object.assign(s.values, {
      integration: "hosted",
      instanceUrl: "https://example.my.salesforce.com",
      environment: "sandbox",
      path: "own",
      clientId: "key",
      clientSecret: "secret",
    });
    saveProviderProgress("sf", {
      ...s,
      saved: true,
      authorized: true,
      tested: true,
    });
    const restored = loadProviderProgress("salesforce", "sf");
    expect(restored.values.integration).toBe("hosted");
    expect(restored.values.instanceUrl).toBe(
      "https://example.my.salesforce.com",
    );
    expect(restored.values.clientSecret).toBe("");
    expect(restored.saved || restored.authorized || restored.tested).toBe(
      false,
    );
  });
});
