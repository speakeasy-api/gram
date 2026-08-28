import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";

import { PlatformMCPInstallWalkthrough } from "./platform-mcp-install-walkthrough";
import { platformMCPMarketplaceRepoURL } from "./platform-mcp-marketplace";

// The real CodeBlock reads theme config from a provider this unit test has no
// reason to mount; the snippet text is what the assertions are about.
vi.mock("@/components/code", () => ({
  CodeBlock: ({ children }: { children: string }) => <pre>{children}</pre>,
}));
vi.mock("@/lib/utils", async (importOriginal) => ({
  ...(await importOriginal<Record<string, unknown>>()),
  getServerURL: () => "https://localhost:8080",
}));
vi.mock("./platform-mcp-marketplace", () => ({
  platformMCPMarketplaceRepoURL: vi.fn((isDevelopment = true) =>
    isDevelopment
      ? "https://localhost:8080/marketplace/local-platform-mcp-marketplace-000000000000.git"
      : "https://github.com/speakeasy-api/marketplace",
  ),
}));

const MCP_URL = "https://app.example.com/mcp/platform";

const methodButton = (label: string | RegExp) =>
  screen.getByRole("button", { name: label }) as HTMLButtonElement;

afterEach(cleanup);

describe("PlatformMCPInstallWalkthrough", () => {
  it("offers the other agent only the remote MCP configuration", () => {
    render(
      <PlatformMCPInstallWalkthrough initialClient="other" mcpUrl={MCP_URL} />,
    );

    // No reviewed package is built for an uncertified agent, so the marketplace
    // route is closed even though it is open to every certified agent.
    expect(methodButton(/Marketplace install/).disabled).toBe(true);

    expect(
      screen.getByText("Add Platform MCP as a remote MCP server"),
    ).toBeTruthy();
    expect(screen.getByText(new RegExp(MCP_URL))).toBeTruthy();
  });

  it("tells the other agent no certified package exists at all", () => {
    render(
      <PlatformMCPInstallWalkthrough initialClient="other" mcpUrl={MCP_URL} />,
    );

    expect(
      screen.getByText(/This agent has no certified plugin package/),
    ).toBeTruthy();
  });

  it("keeps the other agent on the MCP config even when asked for the marketplace", () => {
    // initialMethod names a route the agent has no package for. The walkthrough
    // must not honour it.
    render(
      <PlatformMCPInstallWalkthrough
        initialClient="other"
        initialMethod="marketplace"
        mcpUrl={MCP_URL}
      />,
    );

    expect(
      screen.getByText("Add Platform MCP as a remote MCP server"),
    ).toBeTruthy();
    expect(screen.queryByText(/marketplace add/)).toBeNull();
  });

  it("keeps the marketplace route open for a certified agent", () => {
    render(
      <PlatformMCPInstallWalkthrough
        initialClient="claude_code"
        mcpUrl={MCP_URL}
      />,
    );

    expect(methodButton(/Marketplace install/).disabled).toBe(false);
  });

  it("tells Claude Code users how to open OAuth explicitly", () => {
    render(
      <PlatformMCPInstallWalkthrough
        initialClient="claude_code"
        mcpUrl={MCP_URL}
      />,
    );

    expect(screen.getByText(/Open \/mcp in Claude Code/)).toBeTruthy();
    expect(
      screen.getByText(/Restarting Claude Code alone may not/),
    ).toBeTruthy();
  });

  it("makes restart conditional for an unknown agent", () => {
    render(
      <PlatformMCPInstallWalkthrough initialClient="other" mcpUrl={MCP_URL} />,
    );

    expect(screen.getByText(/Restart your agent if needed/)).toBeTruthy();
  });

  it("installs a certified agent from the local marketplace in development", () => {
    vi.mocked(platformMCPMarketplaceRepoURL).mockReturnValueOnce(
      "https://localhost:8080/marketplace/local-platform-mcp-marketplace-000000000000.git",
    );
    render(
      <PlatformMCPInstallWalkthrough
        initialClient="claude_code"
        mcpUrl={MCP_URL}
      />,
    );

    expect(
      screen.getByText(
        "/plugin marketplace add https://localhost:8080/marketplace/local-platform-mcp-marketplace-000000000000.git",
      ),
    ).toBeTruthy();
    expect(
      screen.getByText("/plugin install speakeasy@speakeasy"),
    ).toBeTruthy();
  });
});
