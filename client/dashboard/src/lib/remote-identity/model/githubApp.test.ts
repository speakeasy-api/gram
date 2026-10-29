import { describe, expect, it } from "vitest";
import { isGitHubMcpUrl } from "./githubApp";

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
  ])("rejects %s", (url) => expect(isGitHubMcpUrl(url)).toBe(false));
});
