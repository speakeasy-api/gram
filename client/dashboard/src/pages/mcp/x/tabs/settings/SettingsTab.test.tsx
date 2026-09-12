import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SettingsTab } from "./SettingsTab";

vi.mock("./sections/BrandingSection", () => ({
  BrandingSection: ({ title = "Branding" }: { title?: string }) => (
    <h2>{title}</h2>
  ),
}));
vi.mock("./sections/authentication/AuthenticationSection", () => ({
  MCP_AUTHENTICATION_SECTION_ID: "authentication",
  AuthenticationSection: ({ mcpServer }: { mcpServer: McpServer }) => (
    <h2>{mcpServer.remoteMcpServerId ? "Identity" : "Authentication"}</h2>
  ),
}));
vi.mock("./sections/DangerZoneSection", () => ({
  DangerZoneSection: () => <h2>Danger Zone</h2>,
}));
vi.mock("./sections/HeadersSection", () => ({
  HeadersSection: () => <h2>Custom Headers</h2>,
}));
vi.mock("./sections/PublicRateLimitsSection", () => ({
  PublicRateLimitsSection: () => <h2>Public Rate Limits</h2>,
}));
vi.mock("./sections/RemoteMcpSessionsSection", () => ({
  RemoteMcpSessionsSection: () => <h2>Sessions</h2>,
}));
vi.mock("./sections/ServerUrlSection", () => ({
  MCP_SERVER_URL_SECTION_ID: "server-url",
  ServerUrlSection: () => <h2>Server URL</h2>,
}));
vi.mock("./sections/ToolFilteringSection", () => ({
  ToolFilteringSection: () => <h2>Tool Filtering</h2>,
}));

function server(overrides: Partial<McpServer>): McpServer {
  return {
    id: "mcp-server-1",
    projectId: "project-1",
    name: "Example server",
    networkAccessMode: "public_only",
    visibility: "private",
    createdAt: new Date(0),
    updatedAt: new Date(0),
    ...overrides,
  } as McpServer;
}

function renderSettings(mcpServer: McpServer): string[] {
  render(
    <MemoryRouter>
      <SettingsTab
        mcpServer={mcpServer}
        endpoints={[]}
        isLoadingEndpoints={false}
      />
    </MemoryRouter>,
  );
  return screen
    .getAllByRole("heading")
    .map((heading) => heading.textContent ?? "");
}

afterEach(cleanup);

describe("SettingsTab", () => {
  it("orders Remote MCP settings around display, identity, and sessions", () => {
    expect(
      renderSettings(
        server({
          remoteMcpServerId: "remote-source-1",
          userSessionIssuerId: "issuer-1",
        }),
      ),
    ).toEqual([
      "Display",
      // Upstream headers are the Identity panel's Custom Headers disclosure
      // not a section of their own.
      "Identity",
      "Server URL",
      "Sessions",
      "Tool Filtering",
      "Danger Zone",
    ]);
  });

  it("preserves tunneled MCP settings structure", () => {
    expect(
      renderSettings(server({ tunneledMcpServerId: "tunneled-source-1" })),
    ).toEqual([
      "Branding",
      "Server URL",
      "Authentication",
      "Public Rate Limits",
      "Tool Filtering",
      "Danger Zone",
    ]);
  });

  it("preserves unproxied MCP settings structure", () => {
    expect(
      renderSettings(server({ unproxiedMcpServerId: "unproxied-source-1" })),
    ).toEqual(["Branding", "Authentication", "Danger Zone"]);
  });
});
