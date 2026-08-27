import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";

import { PlatformMCPInstallWalkthrough } from "./platform-mcp-install-walkthrough";

// A fully provisioned organization: both packaged routes are on offer, so a
// closed route in these tests is the agent's doing rather than the org's.
const packageStatus = vi.hoisted(() => ({
  current: {
    freshness: "current",
    marketplaceName: "acme",
    marketplaceUrl: "https://github.com/acme/marketplace",
    repoUrl: "https://github.com/acme/marketplace",
    directDownloadAvailable: true,
    repairAllowed: false,
    admission: "admitted",
  } as Record<string, unknown>,
}));

vi.mock("@gram/client/react-query/platformMCPPackageStatus.js", () => ({
  usePlatformMCPPackageStatus: () => ({
    data: packageStatus.current,
    isLoading: false,
  }),
  invalidateAllPlatformMCPPackageStatus: vi.fn(),
}));
vi.mock("@gram/client/react-query/repairPlatformMCPPackage.js", () => ({
  useRepairPlatformMCPPackageMutation: () => ({
    mutate: vi.fn(),
    isPending: false,
  }),
}));
vi.mock("@tanstack/react-query", () => ({ useQueryClient: () => ({}) }));
vi.mock("@/contexts/Fetcher", () => ({
  useFetcher: () => ({ fetch: vi.fn() }),
}));
vi.mock("../plugins/downloadPluginPackage", () => ({
  downloadResponse: vi.fn(),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
// The real CodeBlock reads theme config from a provider this unit test has no
// reason to mount; the snippet text is what the assertions are about.
vi.mock("@/components/code", () => ({
  CodeBlock: ({ children }: { children: string }) => <pre>{children}</pre>,
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

    // No reviewed package is built for an uncertified agent, so neither
    // packaged route is selectable even though the organization has both.
    expect(methodButton(/GitHub installation/).disabled).toBe(true);
    expect(methodButton(/Direct .* ZIP/).disabled).toBe(true);

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

  it("keeps the packaged routes open for a certified agent", () => {
    render(
      <PlatformMCPInstallWalkthrough
        initialClient="claude_code"
        mcpUrl={MCP_URL}
      />,
    );

    expect(methodButton(/GitHub installation/).disabled).toBe(false);
    expect(methodButton(/Direct .* ZIP/).disabled).toBe(false);
  });
});
