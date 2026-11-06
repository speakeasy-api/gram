import { describe, expect, it } from "vitest";
import { gitHubAppReady, isGitHubMcpUrl } from "./githubApp";

describe("GitHub MCP URL eligibility", () => {
  it.each([
    "https://api.githubcopilot.com/mcp",
    "https://github.com/mcp",
    "https://api.github.com/mcp",
    "https://API.GITHUBCOPILOT.COM:443/mcp",
  ])("accepts %s", (url) => expect(isGitHubMcpUrl(url)).toBe(true));
  it.each([
    undefined,
    "",
    "not a URL",
    "http://api.githubcopilot.com/mcp",
    "https://github.com.example.net/mcp",
    "https://example.net/github.com",
    "https://github.com@example.net/mcp",
    "https://user@github.com/mcp",
    "https://mcp.linear.app/mcp",
    "https://workspace.google.com/mcp",
    "https://git.example.com/mcp",
    "https://github.com:8443/mcp",
    "https://api.github.com:8443/mcp",
    "https://api.githubcopilot.com:8443/mcp",
  ])("rejects %s", (url) => expect(isGitHubMcpUrl(url)).toBe(false));
});

describe("GitHub App credential limits", () => {
  const draft = {
    instanceUrl: "https://github.com",
    appId: "123",
    installationId: "456",
    privateKey: "-----BEGIN PRIVATE KEY-----",
  };

  it.each(["appId", "installationId"] as const)(
    "bounds %s to a positive signed int64",
    (field) => {
      for (const value of ["1", "9223372036854775807"]) {
        expect(gitHubAppReady({ ...draft, [field]: value })).toBe(true);
      }
      for (const value of [
        "0",
        "01",
        "-1",
        "+1",
        "1.5",
        "1e3",
        "9223372036854775808",
        "9999999999999999999",
        "10000000000000000000",
      ]) {
        expect(gitHubAppReady({ ...draft, [field]: value })).toBe(false);
      }
    },
  );

  it("bounds private keys to 32768 UTF-8 bytes", () => {
    const privateKey = draft.privateKey.padEnd(32768, "x");
    expect(gitHubAppReady({ ...draft, privateKey })).toBe(true);
    expect(gitHubAppReady({ ...draft, privateKey: privateKey + "x" })).toBe(
      false,
    );
    expect(
      gitHubAppReady({
        ...draft,
        privateKey: privateKey.slice(0, -1) + "é",
      }),
    ).toBe(false);
  });
});
