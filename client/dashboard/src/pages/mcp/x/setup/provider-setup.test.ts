import { describe, expect, it, vi } from "vitest";
import { initialState } from "@/pages/catalog/provider-setup/state";
import {
  setupProviderForUrl,
  providerSetupInstallTarget,
  saveProviderProgress,
  loadProviderProgress,
  providerProgressKey,
} from "./provider-setup";

describe("Slack-only setup routing", () => {
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
    "https://api.githubcopilot.com/mcp",
    "https://api.salesforce.com/platform/mcp/v1/platform/sobject-reads",
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
      saved: true,
      authorized: true,
      tested: true,
      complete: true,
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
    const restored = loadProviderProgress("slack", key);
    expect(restored.values.appChoice).toBe("existing");
    expect(restored.values.access).toBe("read");
    expect(restored.values.clientId).toBe("");
    expect(restored.values.clientSecret).toBe("");
    expect([
      restored.saved,
      restored.authorized,
      restored.tested,
      restored.complete,
    ]).toEqual([false, false, false, false]);
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
  it("ignores completion and credential/test markers in older snapshots", () => {
    sessionStorage.setItem(
      "legacy",
      JSON.stringify({
        revision: 1,
        complete: true,
        done: ["begin", "app", "credentials", "test"],
        appChoice: "existing",
        access: "read",
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
