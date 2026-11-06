import { describe, expect, it, vi } from "vitest";
import { initialState } from "@/pages/catalog/provider-setup/state";
import {
  setupProviderForUrl,
  providerSetupInstallTarget,
  saveProviderProgress,
  loadProviderProgress,
  providerProgressKey,
  providerSourceFingerprint,
} from "./provider-setup";

describe("Salesforce source-bound progress", () => {
  const source = {
    url: "https://api.salesforce.com/platform/mcp/v1/platform/sobject-reads",
    instanceUrl: "",
  };
  const baseline = providerSourceFingerprint("upstream", source);
  it("retains an intentional pending draft against the same source", () => {
    const state = initialState("salesforce");
    state.configChanged = true;
    state.sourceFingerprint = baseline;
    state.values.salesforceServer = "sobject-all";
    state.step = "credentials";
    saveProviderProgress("sf-draft", state);
    const restored = loadProviderProgress(
      "salesforce",
      "sf-draft",
      initialState("salesforce").values,
      baseline,
    );
    expect(restored.values.salesforceServer).toBe("sobject-all");
    expect(restored.configChanged).toBe(true);
    expect(restored.step).toBe("credentials");
  });
  it("retains the server and environment when only the saved domain is invalid", () => {
    const state = initialState("salesforce");
    state.configChanged = true;
    state.sourceFingerprint = baseline;
    Object.assign(state.values, {
      environment: "sandbox",
      salesforceServer: "data360",
      salesforceDomain: "example.my.salesforce.com",
    });
    saveProviderProgress("sf-invalid-domain", state);
    const restored = loadProviderProgress(
      "salesforce",
      "sf-invalid-domain",
      initialState("salesforce").values,
      baseline,
    );
    expect(restored.values.environment).toBe("sandbox");
    expect(restored.values.salesforceServer).toBe("data360");
    expect(restored.values.salesforceDomain).toBe("");
    expect(restored.saved).toBe(false);
  });
  it.each([false, true])(
    "uses the current source when its baseline changes (pending=%s)",
    (pending) => {
      const state = initialState("salesforce");
      state.configChanged = pending;
      state.sourceFingerprint = baseline;
      state.values.salesforceServer = "sobject-all";
      state.done = ["choices", "app"];
      state.step = "credentials";
      saveProviderProgress("sf-stale", state);
      const defaults = {
        ...state.values,
        salesforceServer: "sobject-mutations",
      };
      const restored = loadProviderProgress(
        "salesforce",
        "sf-stale",
        defaults,
        providerSourceFingerprint("replacement", source),
      );
      expect(restored.values.salesforceServer).toBe("sobject-mutations");
      expect(restored.step).toBe("choices");
      expect(restored.done).toEqual([]);
      expect(restored.configChanged).toBe(pending);
      expect(restored.saved).toBe(false);
    },
  );
  it("uses server values for a completed draft, even against the same baseline", () => {
    const state = initialState("salesforce");
    state.sourceFingerprint = baseline;
    state.values.salesforceServer = "sobject-all";
    saveProviderProgress("sf-completed", state);
    expect(
      loadProviderProgress(
        "salesforce",
        "sf-completed",
        initialState("salesforce").values,
        baseline,
      ).values.salesforceServer,
    ).toBe("sobject-reads");
  });
  it("retains pending configuration against the advanced source baseline", () => {
    const state = initialState("salesforce");
    const partial = {
      ...source,
      url: "https://api.salesforce.com/platform/mcp/v1/sandbox/platform/sobject-all",
    };
    const partialBaseline = providerSourceFingerprint("upstream", partial);
    Object.assign(state.values, {
      environment: "sandbox",
      salesforceServer: "sobject-all",
    });
    state.configChanged = true;
    state.sourceFingerprint = partialBaseline;
    saveProviderProgress("sf-partial", state);
    const restored = loadProviderProgress(
      "salesforce",
      "sf-partial",
      {
        ...initialState("salesforce").values,
        environment: "sandbox",
        salesforceServer: "sobject-all",
      },
      partialBaseline,
    );
    expect(restored.values.environment).toBe("sandbox");
    expect(restored.values.salesforceServer).toBe("sobject-all");
    expect(restored.configChanged).toBe(true);
    expect(restored.saved).toBe(false);
  });
  it.each([1, 2, 3])(
    "drops obsolete revision %s prototype branches instead of restoring them",
    (revision) => {
      sessionStorage.setItem(
        "sf-prototype",
        JSON.stringify({
          revision,
          step: "support",
          done: ["begin", "choices", "install"],
          values: {
            integration: "hosted",
            path: "support",
            appChoice: "existing",
            instanceUrl: "https://example.my.salesforce.com",
            salesforceServer: "data360",
            environment: "sandbox",
          },
        }),
      );
      const restored = loadProviderProgress("salesforce", "sf-prototype");
      expect(restored.values.integration).toBe("native");
      expect(restored.values.path).toBe("own");
      expect(restored.values.appChoice).toBe("new");
      expect(restored.values.instanceUrl).toBe("");
      expect(restored.values.salesforceServer).toBe("sobject-reads");
      expect(restored.saved || restored.authorized || restored.tested).toBe(
        false,
      );
      expect(["begin", "choices"]).toContain(restored.step);
      expect(restored.done).not.toContain("install");
    },
  );
  it("persists documented app paths and native Salesforce choices in revision 4", () => {
    const state = initialState("salesforce");
    Object.assign(state.values, {
      clientId: "private-id",
      clientSecret: "private-secret",
      integration: "hosted",
      path: "support",
      instanceUrl: "https://example.my.salesforce.com",
      appChoice: "existing",
    });
    saveProviderProgress("sf-current", state);
    const saved = JSON.parse(sessionStorage.getItem("sf-current")!);
    expect(saved.revision).toBe(4);
    expect(saved.values).not.toHaveProperty("integration");
    expect(saved.values.path).toBe("support");
    expect(saved.values).not.toHaveProperty("instanceUrl");
    expect(saved.values.appChoice).toBe("existing");
    expect(sessionStorage.getItem("sf-current")).not.toContain("private-");
  });
  it("ignores a legacy hosted integration field while retaining documented app and server choices", () => {
    sessionStorage.setItem(
      "sf-injected",
      JSON.stringify({
        revision: 4,
        step: "support",
        done: ["install"],
        values: {
          integration: "hosted",
          path: "support",
          appChoice: "existing",
          instanceUrl: "https://example.my.salesforce.com",
          salesforceServer: "data360",
          environment: "sandbox",
        },
      }),
    );
    const state = loadProviderProgress("salesforce", "sf-injected");
    expect(state.values.integration).toBe("native");
    expect(state.values.path).toBe("support");
    expect(state.values.appChoice).toBe("existing");
    expect(state.values.instanceUrl).toBe("");
    expect(state.values.salesforceServer).toBe("data360");
    expect(state.step).toBe("support");
    expect(state.done).toContain("install");
  });
});

describe("Provider setup routing", () => {
  it("recognizes only the official Slack endpoint", () => {
    expect(setupProviderForUrl("https://mcp.slack.com/mcp/")).toBe("slack");
    for (const url of [
      "https://mcp.slack.com.evil.test/mcp",
      "http://mcp.slack.com/mcp",
      "https://mcp.slack.com/other",
      "https://mcp.slack.com/mcp?other=1",
      "https://user@mcp.slack.com/mcp",
      "not a URL",
    ])
      expect(setupProviderForUrl(url)).toBeNull();
  });
  it.each(["https://example.okta.com/mcp"])(
    "leaves other providers on their existing path: %s",
    (url) => {
      expect(setupProviderForUrl(url)).toBeNull();
      expect(
        providerSetupInstallTarget(
          [{ remotes: [{ url }] }],
          [
            {
              status: "completed",
              key: "server",
              name: "Server",
              identityMode: "user",
              remoteUrl: url,
              mcpServerId: "id",
              mcpServerParam: "server",
            },
          ],
        ),
      ).toBeNull();
    },
  );
  it("checks the installed remote rather than another endpoint offered by the catalog entry", () => {
    const servers = [
      {
        remotes: [
          { url: "https://mcp.slack.com/mcp" },
          { url: "https://other.example/mcp" },
        ],
      },
    ];
    const status = {
      status: "completed" as const,
      key: "selected",
      name: "Selected",
      identityMode: "user" as const,
      mcpServerId: "id",
      mcpServerParam: "selected",
      remoteUrl: "https://other.example/mcp",
    };
    expect(providerSetupInstallTarget(servers, [status])).toBeNull();
    expect(
      providerSetupInstallTarget(servers, [
        { ...status, remoteUrl: "https://mcp.slack.com/mcp" },
      ]),
    ).toBe("selected");
  });
  it("opens Slack setup only for User Identity", () => {
    const servers = [{ remotes: [{ url: "https://mcp.slack.com/mcp" }] }];
    const status = {
      status: "completed" as const,
      key: "slack",
      remoteUrl: "https://mcp.slack.com/mcp",
      name: "Slack",
      mcpServerId: "id",
      mcpServerParam: "installed",
    };
    expect(
      providerSetupInstallTarget(servers, [
        { ...status, identityMode: "user" },
      ]),
    ).toBe("installed");
    for (const identityMode of ["agent", "none", undefined] as const)
      expect(
        providerSetupInstallTarget(servers, [{ ...status, identityMode }]),
      ).toBeNull();
    expect(
      providerSetupInstallTarget(servers, [
        {
          ...status,
          identityMode: "user",
          status: "failed",
          setupRequired: "Configure identity",
        },
      ]),
    ).toBe("installed");
    expect(
      providerSetupInstallTarget(servers, [
        { ...status, identityMode: "user", status: "failed" },
      ]),
    ).toBeNull();
    expect(
      providerSetupInstallTarget(servers, [
        { ...status, identityMode: "user", mcpServerId: undefined },
      ]),
    ).toBeNull();
    expect(
      providerSetupInstallTarget(
        [...servers, ...servers],
        [{ ...status, identityMode: "user" }],
      ),
    ).toBeNull();
  });
});
describe("Slack progress", () => {
  it("retains choices without credentials, authorization, test passes or completion", () => {
    const key = providerProgressKey(
        "slack",
        "user",
        "org",
        "project",
        "server",
      ),
      state = initialState("slack");
    saveProviderProgress(key, {
      ...state,
      step: "test",
      accessChosen: true,
      saved: true,
      authorized: true,
      tested: true,
      done: ["begin", "app", "credentials", "test"],
      values: {
        ...state.values,
        appChoice: "existing",
        access: "read",
        clientId: "private-id",
        clientSecret: "private-secret",
      },
    });
    expect(sessionStorage.getItem(key)).not.toContain("private-");
    expect(JSON.parse(sessionStorage.getItem(key)!)).toEqual({
      revision: 2,
      step: "test",
      done: ["begin", "app"],
      values: { appChoice: "existing", access: "read" },
    });
    const restored = loadProviderProgress("slack", key);
    expect(restored.values.appChoice).toBe("existing");
    expect(restored.values.access).toBe("read");
    expect(restored.accessChosen).toBe(true);
    expect(restored.values.clientId).toBe("");
    expect(restored.values.clientSecret).toBe("");
    expect([restored.saved, restored.authorized, restored.tested]).toEqual([
      false,
      false,
      false,
    ]);
    expect(restored.done).toEqual(["begin", "app"]);
    expect(restored.screen).toBe("setup");
    expect(
      loadProviderProgress(
        "slack",
        providerProgressKey("slack", "other-user", "org", "project", "server"),
      ).step,
    ).toBe("begin");
    expect(
      loadProviderProgress(
        "slack",
        providerProgressKey("slack", "user", "org", "project", "other-server"),
      ).step,
    ).toBe("begin");
  });
  it("ignores untrusted credential/test markers in a choice snapshot", () => {
    sessionStorage.setItem(
      "legacy",
      JSON.stringify({
        revision: 1,
        done: ["begin", "app", "credentials", "test"],
        values: { appChoice: "existing", access: "read" },
      }),
    );
    const state = loadProviderProgress("slack", "legacy");
    expect(state.screen).toBe("setup");
    expect(state.done).toEqual(["begin", "app"]);
    expect(state.values.access).toBe("read");
  });
  it("handles corrupt browser storage", () => {
    sessionStorage.setItem("broken", "invalid");
    expect(loadProviderProgress("slack", "broken").step).toBe("begin");
  });
  it("continues in memory when browser storage is unavailable", () => {
    const get = vi
      .spyOn(Storage.prototype, "getItem")
      .mockImplementation(() => {
        throw new Error("Unavailable");
      });
    const set = vi
      .spyOn(Storage.prototype, "setItem")
      .mockImplementation(() => {
        throw new Error("Unavailable");
      });
    try {
      expect(loadProviderProgress("slack", "disabled").step).toBe("begin");
      expect(() =>
        saveProviderProgress("disabled", initialState("slack")),
      ).not.toThrow();
    } finally {
      get.mockRestore();
      set.mockRestore();
    }
  });
});

describe("GitHub setup routing and browser progress", () => {
  it("recognizes only the official GitHub MCP endpoint", () => {
    expect(setupProviderForUrl("https://api.githubcopilot.com/mcp/")).toBe(
      "github",
    );
    for (const url of [
      "http://api.githubcopilot.com/mcp",
      "https://api.githubcopilot.com.evil.test/mcp",
      "https://api.githubcopilot.com/other",
      "https://api.githubcopilot.com/mcp?x=1",
      "https://user@api.githubcopilot.com/mcp",
      "https://api.githubcopilot.com:444/mcp",
    ])
      expect(setupProviderForUrl(url)).toBeNull();
  });
  it.each(["user", "agent", "none"] as const)(
    "hands off only User Identity (%s)",
    (identityMode) => {
      expect(
        providerSetupInstallTarget(
          [{}],
          [
            {
              status: "completed",
              key: "github",
              name: "GitHub",
              remoteUrl: "https://api.githubcopilot.com/mcp",
              mcpServerId: "id",
              mcpServerParam: "installed",
              identityMode,
            },
          ],
        ),
      ).toBe(identityMode === "user" ? "installed" : null);
    },
  );
  it("restores organization and app choice without credentials or verified results", () => {
    const state = initialState("github");
    saveProviderProgress("github-test", {
      ...state,
      step: "app",
      saved: true,
      authorized: true,
      tested: true,
      values: {
        ...state.values,
        org: "example-org",
        appChoice: "existing",
        clientId: "private-id",
        clientSecret: "private-secret",
      },
    });
    const restored = loadProviderProgress("github", "github-test");
    expect(restored.step).toBe("app");
    expect(restored.values.org).toBe("example-org");
    expect(restored.values.appChoice).toBe("existing");
    expect(restored.values.clientId).toBe("");
    expect(restored.values.clientSecret).toBe("");
    expect(sessionStorage.getItem("github-test")).not.toContain("private-");
    expect([restored.saved, restored.authorized, restored.tested]).toEqual([
      false,
      false,
      false,
    ]);
  });
});

it("does not restore an unconfirmed default as an explicit permission choice", () => {
  const key = "unconfirmed-permissions";
  saveProviderProgress(key, initialState("slack"));
  expect(JSON.parse(sessionStorage.getItem(key)!).values).not.toHaveProperty(
    "access",
  );
  expect(loadProviderProgress("slack", key).accessChosen).toBe(false);
  sessionStorage.removeItem(key);
});

it.each(["all", "read"])(
  "restores legacy %s access conservatively",
  (access) => {
    const key = "legacy-access";
    sessionStorage.setItem(
      key,
      JSON.stringify({
        revision: 1,
        values: { appChoice: "existing", access },
      }),
    );
    const restored = loadProviderProgress("slack", key);
    expect(restored.values.appChoice).toBe("existing");
    expect(restored.accessChosen).toBe(access === "read");
    expect(restored.values.access).toBe(access);
    sessionStorage.removeItem(key);
  },
);

it.each([
  "https://api.salesforce.com/platform/mcp/v1/platform/sobject-reads",
  "https://api.salesforce.com/platform/mcp/v1/sandbox/platform/sobject-all",
  "https://api.salesforce.com/platform/mcp/v1/data/data360",
  "https://api.salesforce.com/platform/mcp/v1/sandbox/platform/headless-360",
  "https://api.salesforce.com/platform/mcp/v1/analytics/tableau-next",
])("hands off only User Identity Salesforce installs: %s", (url) => {
  const servers = [{ remotes: [{ url }] }];
  const status = {
    status: "completed" as const,
    key: "sf",
    name: "Salesforce",
    remoteUrl: url,
    mcpServerId: "id",
    mcpServerParam: "installed",
  };
  expect(
    providerSetupInstallTarget(servers, [{ ...status, identityMode: "user" }]),
  ).toBe("installed");
  expect(
    providerSetupInstallTarget(servers, [{ ...status, identityMode: "agent" }]),
  ).toBeNull();
  expect(
    providerSetupInstallTarget(servers, [{ ...status, identityMode: "none" }]),
  ).toBeNull();
});

it.each([
  "https://app.getgram.ai/mcp/speakeasy-team-salesforce",
  "https://api.salesforce.com/platform/mcp/v1/sandbox/data/data360",
  "https://api.salesforce.com/platform/mcp/v1/platform/unknown",
])(
  "does not hand an undocumented Salesforce catalog endpoint into guided setup: %s",
  (url) => {
    expect(
      providerSetupInstallTarget(
        [{ remotes: [{ url }] }],
        [
          {
            status: "completed",
            key: "sf",
            name: "Salesforce",
            identityMode: "user",
            remoteUrl: url,
            mcpServerId: "id",
            mcpServerParam: "installed",
          },
        ],
      ),
    ).toBeNull();
  },
);

it("recognizes native Snowflake but excludes the older hosted integration", () => {
  expect(
    setupProviderForUrl(
      "https://example.snowflakecomputing.com/api/v2/databases/A/schemas/B/mcp-servers/C",
    ),
  ).toBe("snowflake");
  expect(
    setupProviderForUrl("https://app.getgram.ai/mcp/speakeasy-team-snowflake"),
  ).toBeNull();
});

describe("Snowflake source-bound progress", () => {
  it("restores pending drafts without credentials and resets after an external edit", () => {
    const state = initialState("snowflake");
    const source = {
      url: "https://example.snowflakecomputing.com/api/v2/databases/APP/schemas/PUBLIC/mcp-servers/MCP",
    };
    const baseline = providerSourceFingerprint("upstream", source);
    Object.assign(state.values, {
      host: "example.snowflakecomputing.com",
      database: "DRAFT",
      schema: "PUBLIC",
      serverName: "MCP",
      agent: "APP.PUBLIC.AGENT",
      clientId: "id",
      clientSecret: "secret",
    });
    state.configChanged = true;
    state.sourceFingerprint = baseline;
    state.step = "credentials";
    saveProviderProgress("snowflake-draft", state);
    const defaults = {
      host: "example.snowflakecomputing.com",
      database: "APP",
      schema: "PUBLIC",
      serverName: "MCP",
    };
    const restored = loadProviderProgress(
      "snowflake",
      "snowflake-draft",
      defaults,
      baseline,
    );
    expect(restored.values.database).toBe("DRAFT");
    expect(restored.values.clientId).toBe("");
    expect(restored.values.clientSecret).toBe("");
    expect(restored.saved).toBe(false);
    expect(restored.configChanged).toBe(true);
    const changed = loadProviderProgress(
      "snowflake",
      "snowflake-draft",
      { ...defaults, database: "OTHER" },
      providerSourceFingerprint("upstream", {
        url: source.url.replace("/APP/", "/OTHER/"),
      }),
    );
    expect(changed.values.database).toBe("OTHER");
    expect(changed.values.agent).toBe("");
    expect(changed.step).toBe("account");
  });
});
