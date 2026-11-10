import { describe, expect, it } from "vitest";
import { initialState, reduce, ready, steps, validate } from "./state";
import {
  salesforceConnection,
  salesforceSelectionForUrl,
  salesforceInstance,
  salesforceAppInstallationUrl,
  salesforceMyDomain,
} from "./salesforce";
import {
  setupProviderForUrl,
  loadProviderProgress,
  saveProviderProgress,
} from "@/pages/mcp/x/setup/provider-setup";

describe("documented Salesforce connection contract", () => {
  it.each([
    [
      "production",
      "https://example.my.salesforce-setup.com/lightning/setup/SetupOneHome/home",
      "https://example.my.salesforce.com",
      "",
    ],
    [
      "production",
      "https://example-dev-ed.develop.my.salesforce-setup.com/",
      "https://example-dev-ed.develop.my.salesforce.com",
      "develop/",
    ],
    [
      "sandbox",
      "example--uat.sandbox.lightning.force.com/lightning/page/home",
      "https://example--uat.sandbox.my.salesforce.com",
      "sandbox/",
    ],
    [
      "production",
      "https://EXAMPLE.my.salesforce.com/path?source=setup#details",
      "https://example.my.salesforce.com",
      "",
    ],
  ])(
    "normalizes a pasted %s Salesforce page URL",
    (environment, input, origin, partition) => {
      const domain = salesforceMyDomain(input, environment);
      expect(domain?.origin).toBe(origin);
      const s = initialState("salesforce");
      Object.assign(s.values, { environment, salesforceDomain: input });
      const connection = salesforceConnection(s.values);
      const name =
        environment === "sandbox"
          ? "example--uat"
          : partition
            ? "example-dev-ed"
            : "example";
      expect(connection?.url).toBe(
        `https://api.salesforce.com/platform/mcp/v1/d/${name}/${partition}platform/sobject-reads`,
      );
      expect(connection?.authorizationEndpoint).toBe(
        `${origin}/services/oauth2/authorize`,
      );
      expect(salesforceSelectionForUrl(connection?.url)).toMatchObject({
        environment,
        salesforceDomain: origin,
      });
    },
  );
  it.each([
    [
      "production",
      "example.my.salesforce.com",
      "d/example/platform/sobject-reads",
      "sobject-reads",
    ],
    [
      "sandbox",
      "example--uat.sandbox.my.salesforce.com",
      "d/example--uat/sandbox/platform/sobject-reads",
      "sobject-reads",
    ],
    [
      "sandbox",
      "example--uat.sandbox.my.salesforce.com",
      "d/example--uat/sandbox/data/data360",
      "data360",
    ],
  ])(
    "uses My Domain for %s login and MCP routing",
    (environment, hostname, path, salesforceServer) => {
      const s = initialState("salesforce");
      Object.assign(s.values, {
        environment,
        salesforceDomain: hostname,
        salesforceServer,
      });
      const url = `https://api.salesforce.com/platform/mcp/v1/${path}`;
      const issuer = `https://${hostname}`;
      expect(salesforceConnection(s.values)).toMatchObject({
        url,
        issuer,
        authorizationEndpoint: `${issuer}/services/oauth2/authorize`,
        tokenEndpoint: `${issuer}/services/oauth2/token`,
        instanceUrl: null,
        authMethod: "none",
      });
      expect(salesforceSelectionForUrl(url)).toMatchObject({
        environment,
        salesforceDomain: issuer,
        salesforceServer,
      });
      expect(setupProviderForUrl(url)).toBe("salesforce");
    },
  );

  it.each([
    "https://example.my.salesforce.com.evil.test",
    "http://example.my.salesforce.com",
    "https://example.my.salesforce-setup.com.evil.test/path",
    "https://u:p@example.my.salesforce.com",
    "https://example.my.salesforce.com:8443",
    "https://example--uat.sandbox.my.salesforce.com",
  ])("blocks invalid or mismatched My Domain input: %s", (salesforceDomain) => {
    const s = initialState("salesforce");
    Object.assign(s.values, { salesforceDomain });
    expect(salesforceConnection(s.values)).toBeNull();
    expect(
      validate({ ...s, step: "salesforce-org" }).salesforceDomain,
    ).toBeTruthy();
    // Environment choices remain editable so a mismatched domain can be corrected next.
    expect(validate({ ...s, step: "choices" })).toEqual({});
  });
  it.each([
    ["production", "login.salesforce.com"],
    ["sandbox", "test.salesforce.com"],
  ])(
    "opens the %s package installer for the selected org type",
    (environment, host) => {
      expect(salesforceAppInstallationUrl(environment)).toBe(
        `https://${host}/packaging/installPackage.apexp?p0=04tdM000000cNGXQA2`,
      );
    },
  );
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
      ["salesforceDomain", "example.my.salesforce.com"],
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
      "salesforce-org",
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
    const handoff = reduce({ ...s, step: "install" }, { type: "next" });
    expect(handoff.step).toBe("support");
    expect(reduce(handoff, { type: "next" }).step).toBe("support");
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
      salesforceDomain: "https://example--uat.sandbox.my.salesforce.com",
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
    expect(restored.values.salesforceDomain).toBe(
      "https://example--uat.sandbox.my.salesforce.com",
    );
    expect(restored.values.clientId).toBe("");
    expect(restored.values.clientSecret).toBe("");
    expect(restored.saved || restored.authorized || restored.tested).toBe(
      false,
    );
  });
});
