import { describe, expect, it, vi } from "vitest";
import { initialState } from "@/pages/catalog/provider-setup/state";
import {
  setupProviderForUrl,
  providerSetupInstallTarget,
  saveProviderProgress,
  loadProviderProgress,
  providerProgressKey,
} from "./provider-setup";

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
  it.each([
    "https://example.snowflakecomputing.com/api/v2/databases/A/schemas/B/mcp-servers/C",
    "https://example.okta.com/mcp",
  ])("leaves other providers on their existing path: %s", (url) => {
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
  });
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
  "https://app.getgram.ai/mcp/speakeasy-team-salesforce",
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
