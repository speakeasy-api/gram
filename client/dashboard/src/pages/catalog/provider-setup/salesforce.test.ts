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

describe("documented Salesforce connection contract", () => {
  it.each([
    [
      "sobject-reads",
      "platform/sobject-reads",
      "sandbox/platform/sobject-reads",
    ],
    [
      "sobject-mutations",
      "platform/sobject-mutations",
      "sandbox/platform/sobject-mutations",
    ],
    [
      "sobject-deletes",
      "platform/sobject-deletes",
      "sandbox/platform/sobject-deletes",
    ],
    ["sobject-all", "platform/sobject-all", "sandbox/platform/sobject-all"],
    ["data360", "data/data360", "data/sandbox/data360"],
    ["headless-360", "platform/headless-360", "sandbox/platform/headless-360"],
    [
      "tableau-next",
      "analytics/tableau-next",
      "sandbox/analytics/tableau-next",
    ],
  ])(
    "maps documented %s production and sandbox endpoints to the native public-client flow",
    (id, production, sandbox) => {
      for (const [environment, path] of [
        ["production", production],
        ["sandbox", sandbox],
      ]) {
        const url = `https://api.salesforce.com/platform/mcp/v1/${path}`;
        const selection = {
          integration: "native",
          environment,
          salesforceServer: id,
        };
        expect(salesforceSelectionForUrl(url)).toEqual(selection);
        expect(setupProviderForUrl(url)).toBe("salesforce");
        const s = initialState("salesforce");
        Object.assign(s.values, selection, { clientId: "key" });
        expect(salesforceConnection(s.values)).toMatchObject({
          url,
          issuer:
            environment === "sandbox"
              ? "https://test.salesforce.com"
              : "https://login.salesforce.com",
          scopes: ["mcp_api", "refresh_token"],
          authMethod: "none",
          instanceUrl: null,
        });
        expect(validate({ ...s, step: "credentials" })).toEqual({});
      }
    },
  );

  it.each([
    "https://app.getgram.ai/mcp/speakeasy-team-salesforce",
    "https://api.salesforce.com.evil.test/platform/mcp/v1/platform/sobject-reads",
    "https://api.salesforce.com/platform/mcp/v1/platform/sobject-reads?x=1",
    "https://api.salesforce.com/platform/mcp/v1/platform/unknown",
    "https://app.getgram.ai/mcp/other-salesforce",
  ])(
    "leaves undocumented or unrelated endpoints outside guided setup: %s",
    (url) => {
      expect(salesforceSelectionForUrl(url)).toBeNull();
      expect(setupProviderForUrl(url)).toBeNull();
    },
  );

  it.each([
    { integration: "hosted", instanceUrl: "https://example.my.salesforce.com" },
    { salesforceServer: "unknown" },
    { environment: "unknown" },
  ])("rejects unsupported retained choices: %j", (values) => {
    const s = initialState("salesforce");
    Object.assign(s.values, values, { clientId: "key" });
    expect(salesforceConnection(s.values)).toBeNull();
    expect(ready({ ...s, saved: true, authorized: true, tested: true })).toBe(
      false,
    );
  });

  it.each([
    "http://example.my.salesforce.com",
    "https://salesforce.com.evil.test",
    "https://example.my.salesforce.com/path",
    "https://u:p@example.my.salesforce.com",
    "https://example.my.salesforce.com?token=x",
    "https://example.my.salesforce.com:8443",
  ])("rejects unsafe instance values on existing source headers: %s", (url) => {
    expect(salesforceInstance(url)).toBeNull();
  });

  it("accepts the documented Consumer Key-only configuration", () => {
    const s = { ...initialState("salesforce"), step: "credentials" };
    s.values.clientId = "key";
    expect(validate(s)).toEqual({});
    expect(salesforceConnection(s.values)?.authMethod).toBe("none");
  });

  it("invalidates credentials and verification when server or environment changes", () => {
    for (const [field, value] of [
      ["environment", "sandbox"],
      ["salesforceServer", "sobject-all"],
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

  it("keeps activation and connection verification in the documented sequence", () => {
    const s = initialState("salesforce");
    expect(steps(s).map((x) => x.id)).toEqual([
      "begin",
      "choices",
      "app",
      "oauth",
      "security",
      "activation",
      "credentials",
      "test",
    ]);
    expect(
      reduce(
        { ...s, saved: true, authorized: true, tested: false },
        { type: "complete" },
      ).screen,
    ).toBe("setup");
    expect(
      reduce(
        { ...s, saved: true, authorized: true, tested: true },
        { type: "complete" },
      ).screen,
    ).toBe("success");
  });

  it("describes the selected native endpoint on the documented support path without granting readiness or completion", () => {
    const s = initialState("salesforce");
    s.values.path = "support";
    s.values.environment = "sandbox";
    s.values.salesforceServer = "data360";
    expect(salesforceConnection(s.values)?.url).toBe(
      "https://api.salesforce.com/platform/mcp/v1/data/sandbox/data360",
    );
    const rail = steps(s).map((step) => step.id);
    expect(rail).toContain("install");
    expect(rail).toContain("support");
    expect(rail).not.toContain("credentials");
    expect(ready({ ...s, saved: true, authorized: true, tested: true })).toBe(
      false,
    );
    expect(
      reduce(
        { ...s, saved: true, authorized: true, tested: true },
        { type: "complete" },
      ).screen,
    ).toBe("setup");
  });

  it("restores documented choices without credentials or verified results", () => {
    const s = initialState("salesforce");
    Object.assign(s.values, {
      environment: "sandbox",
      salesforceServer: "sobject-deletes",
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
    expect(restored.values.environment).toBe("sandbox");
    expect(restored.values.salesforceServer).toBe("sobject-deletes");
    expect(restored.values.clientId).toBe("");
    expect(restored.values.clientSecret).toBe("");
    expect(restored.saved || restored.authorized || restored.tested).toBe(
      false,
    );
  });
});
