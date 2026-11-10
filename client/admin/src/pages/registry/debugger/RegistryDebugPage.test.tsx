import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { createRootRoute, createRoute } from "@tanstack/react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderRouteTree } from "@/test/harness";
import { registryDebugSearch } from "./debugSearch";
import { RegistryDebugPage } from "./RegistryDebugPage";

const mocks = vi.hoisted(() => ({
  debug: vi.fn(),
  entry: vi.fn(),
  verifyLink: vi.fn(),
}));

vi.mock("@/lib/gramAdminClient", () => ({
  adminDebugMcpUrl: mocks.debug,
  adminGetMcpVerificationLink: mocks.verifyLink,
  registryEntryQuery: (id: string) => ({
    queryKey: ["registry-entry", id],
    queryFn: () => mocks.entry(id),
  }),
}));

const ENTRY_ID = "00000000-0000-4000-8000-000000000001";

function entryWithRemotes(remotes: unknown[]) {
  return {
    id: ENTRY_ID,
    dataJson: JSON.stringify({
      server: { name: "example.test/demo", remotes },
    }),
    published: true,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "opaque",
    issues: [],
  };
}

const REPORT = {
  url: "https://mcp.example.test/mcp",
  outcome: "authentication_required",
  durationMs: 340,
  authMethod: "client_id_metadata_document",
  steps: [
    {
      id: "reach_server",
      title: "Reach server",
      status: "passed",
      summary: "The server answered POST with HTTP 401.",
      durationMs: 120,
      httpStatus: 401,
    },
    {
      id: "detect_auth_method",
      title: "Detect authentication method",
      status: "warning",
      summary: "This provider can't reach Speakeasy's document.",
      details: { registration_endpoint: "https://mcp.example.test/register" },
    },
    {
      id: "initialize",
      title: "Initialize MCP",
      status: "skipped",
      summary: "Sign-in is required.",
    },
  ],
};

function renderPage(path: string) {
  const root = createRootRoute();
  const debug = createRoute({
    getParentRoute: () => root,
    path: "registry/debug",
    component: RegistryDebugPage,
    validateSearch: registryDebugSearch,
  });
  return renderRouteTree(root.addChildren([debug]), { initialPath: path });
}

describe("RegistryDebugPage", () => {
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("traces a one-off URL and leads with the warning", async () => {
    mocks.debug.mockResolvedValue(REPORT);
    mocks.verifyLink.mockResolvedValue({
      url: "https://app.example.test/speakeasy/projects/default/mcp/add/remote?url=x",
      existing: false,
      projectName: "Default",
    });
    const { router } = await renderPage("/registry/debug");

    expect(
      screen.getByRole("heading", { name: "MCP debugger", level: 1 }),
    ).toBeTruthy();
    const run = screen.getByRole("button", { name: "Run" });
    expect((run as HTMLButtonElement).disabled).toBe(true);

    fireEvent.change(screen.getByRole("textbox", { name: "MCP server URL" }), {
      target: { value: "https://mcp.example.test/mcp" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Run" }));

    await screen.findByText("Sign-in required", { selector: "h2" });
    expect(mocks.debug).toHaveBeenCalledWith("https://mcp.example.test/mcp");
    expect(
      screen.getByText("Check Detect authentication method."),
    ).toBeTruthy();
    expect(router.state.location.search).toEqual({
      url: "https://mcp.example.test/mcp",
    });

    fireEvent.click(screen.getByText("Check Detect authentication method."));
    expect(screen.getByText(/registration_endpoint/)).toBeTruthy();
  });

  it("links to sign in and run tools once the server answered", async () => {
    mocks.debug.mockResolvedValue(REPORT);
    mocks.verifyLink.mockResolvedValue({
      url: "https://app.example.test/speakeasy/projects/default/playground?mcpServer=1",
      existing: true,
      projectName: "Default",
    });
    await renderPage(
      "/registry/debug?url=https%3A%2F%2Fmcp.example.test%2Fmcp",
    );

    fireEvent.click(screen.getByRole("button", { name: "Run" }));

    const link = await screen.findByRole("link", {
      name: "Open in Playground",
    });
    expect(link.getAttribute("href")).toBe(
      "https://app.example.test/speakeasy/projects/default/playground?mcpServer=1",
    );
    expect(link.getAttribute("target")).toBe("_blank");
    expect(mocks.verifyLink).toHaveBeenCalledWith(
      "https://mcp.example.test/mcp",
      undefined,
    );
  });

  it("explains when verifying in Speakeasy isn't set up", async () => {
    mocks.debug.mockResolvedValue(REPORT);
    mocks.verifyLink.mockRejectedValue(
      new Error("MCP verification is not configured"),
    );
    await renderPage(
      "/registry/debug?url=https%3A%2F%2Fmcp.example.test%2Fmcp",
    );

    fireEvent.click(screen.getByRole("button", { name: "Run" }));

    await screen.findByText(
      "Verify in Speakeasy is unavailable: MCP verification is not configured",
    );
  });

  it("links the public-address runbook when CIMD can't reach this stack", async () => {
    mocks.debug.mockResolvedValue({
      ...REPORT,
      steps: REPORT.steps.map((step) =>
        step.id === "detect_auth_method"
          ? {
              ...step,
              details: {
                speakeasy_client_metadata_origin: "https://localhost:27699",
              },
            }
          : step,
      ),
    });
    mocks.verifyLink.mockResolvedValue({
      url: "https://app.example.test/x",
      existing: false,
      projectName: "Default",
    });
    await renderPage(
      "/registry/debug?url=https%3A%2F%2Fmcp.example.test%2Fmcp",
    );

    fireEvent.click(screen.getByRole("button", { name: "Run" }));

    const link = await screen.findByRole("link", {
      name: "Set up a public address to test CIMD sign-in locally",
    });
    expect(link.getAttribute("href")).toContain(
      "docs/runbooks/oauth-debugging.md",
    );
  });

  it("leaves out the runbook for other warnings", async () => {
    mocks.debug.mockResolvedValue(REPORT);
    mocks.verifyLink.mockResolvedValue({
      url: "https://app.example.test/x",
      existing: false,
      projectName: "Default",
    });
    await renderPage(
      "/registry/debug?url=https%3A%2F%2Fmcp.example.test%2Fmcp",
    );

    fireEvent.click(screen.getByRole("button", { name: "Run" }));

    await screen.findByText("Sign-in required", { selector: "h2" });
    expect(screen.queryByText(/public address/)).toBeNull();
  });

  it("offers no verification for a server that never answered", async () => {
    mocks.debug.mockResolvedValue({ ...REPORT, outcome: "unreachable" });
    await renderPage(
      "/registry/debug?url=https%3A%2F%2Fmcp.example.test%2Fmcp",
    );

    fireEvent.click(screen.getByRole("button", { name: "Run" }));

    await screen.findByRole("heading", { level: 2 });
    expect(screen.queryByText(/Verify in Speakeasy/)).toBeNull();
    expect(mocks.verifyLink).not.toHaveBeenCalled();
  });

  it("starts from a registry entry's remote", async () => {
    mocks.entry.mockResolvedValue(
      entryWithRemotes([
        { type: "streamable-http", url: "https://mcp.example.test/mcp" },
      ]),
    );
    await renderPage(`/registry/debug?entry=${ENTRY_ID}`);

    await screen.findByRole("heading", {
      name: "Debug example.test/demo",
      level: 1,
    });
    await waitFor(() =>
      expect(
        (
          screen.getByRole("textbox", {
            name: "MCP server URL",
          }) as HTMLInputElement
        ).value,
      ).toBe("https://mcp.example.test/mcp"),
    );
  });

  it("asks for template variables to be filled in before running", async () => {
    mocks.entry.mockResolvedValue(
      entryWithRemotes([
        { type: "streamable-http", url: "https://{tenant}.example.test/mcp" },
      ]),
    );
    await renderPage(`/registry/debug?entry=${ENTRY_ID}`);

    await screen.findByText("{tenant}");
    expect(
      (screen.getByRole("button", { name: "Run" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
  });

  it("shows why the trace didn't run", async () => {
    mocks.debug.mockRejectedValue(new Error("invalid url"));
    await renderPage(
      "/registry/debug?url=https%3A%2F%2Fmcp.example.test%2Fmcp",
    );

    fireEvent.click(screen.getByRole("button", { name: "Run" }));

    await screen.findByText("The trace didn't run");
    expect(screen.getByText("invalid url")).toBeTruthy();
  });
});
