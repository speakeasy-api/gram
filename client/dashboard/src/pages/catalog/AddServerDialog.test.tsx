import { TooltipProvider } from "@/components/ui/Tooltip";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AddServerDialog } from "./AddServerDialog";

const mocks = vi.hoisted(() => ({
  getServerDetails: vi.fn(),
  hasScope: vi.fn(),
  onOpenChange: vi.fn(),
  reset: vi.fn(),
  startInstall: vi.fn(),
  updateServerConfig: vi.fn(),
  workflow: vi.fn(),
}));

vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project-1", slug: "default" }),
}));

vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    mcpRegistries: { getServerDetails: mocks.getServerDetails },
  }),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: mocks.hasScope,
    isLoading: false,
  }),
}));

vi.mock("./useRemoteMcpInstallWorkflow", () => ({
  headerValueKey: (
    serverIndex: number,
    remoteUrl: string,
    headerName: string,
  ) => `${serverIndex}:${remoteUrl}:${headerName}`,
  useRemoteMcpInstallWorkflow: () => mocks.workflow(),
}));

const server = {
  description: "A test server",
  registryId: "",
  registrySpecifier: "test/server",
  title: "Test Server",
  version: "1.0.0",
  meta: {},
  toolCount: 0,
  isReadOnly: false,
  supportsDcr: true,
  remotes: [
    {
      url: "https://mcp.example.test/mcp",
      transportType: "streamable-http" as const,
    },
  ],
};

function renderDialog(): void {
  render(
    <TooltipProvider>
      <AddServerDialog
        servers={[server]}
        open
        onOpenChange={(open) => {
          mocks.onOpenChange(open);
        }}
      />
    </TooltipProvider>,
  );
}

beforeEach(() => {
  mocks.hasScope.mockReturnValue(false);
  mocks.workflow.mockReturnValue({
    phase: "configure",
    projectSlug: "default",
    serverConfigs: [
      {
        server,
        name: "Test Server",
        remotes: server.remotes,
        identityMode: "user",
        agentAuthorization: "",
        headerValues: {},
      },
    ],
    canInstall: true,
    goBack: undefined,
    isServerAlreadyInstalled: () => false,
    reset: mocks.reset,
    startInstall: mocks.startInstall,
    updateServerConfig: mocks.updateServerConfig,
  });
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("AddServerDialog identity permissions", () => {
  it("disables User Identity and explains project:write", async () => {
    renderDialog();

    await waitFor(() =>
      expect(screen.getByRole("radio", { name: "User" })).toBeDefined(),
    );
    expect(
      (screen.getByRole("radio", { name: "User" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    expect(
      screen.getByText(/User Identity creates a provider or OAuth client/i),
    ).toBeDefined();
    expect(
      (
        screen.getByRole("button", {
          name: "Add to Project",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    expect(mocks.hasScope).toHaveBeenCalledWith("project:write", "project-1");
  });

  it("allows User Identity with project:write", async () => {
    mocks.hasScope.mockReturnValue(true);
    renderDialog();

    await waitFor(() =>
      expect(screen.getByRole("radio", { name: "User" })).toBeDefined(),
    );
    expect(
      (screen.getByRole("radio", { name: "User" }) as HTMLButtonElement)
        .disabled,
    ).toBe(false);
    expect(
      screen.queryByText(/User Identity creates a provider or OAuth client/i),
    ).toBeNull();
    expect(
      (
        screen.getByRole("button", {
          name: "Add to Project",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(false);
  });

  it("does not apply identity permissions to Figma", async () => {
    const figmaServer = {
      ...server,
      registrySpecifier: "com.figma.mcp/mcp",
    };
    mocks.workflow.mockReturnValue({
      ...mocks.workflow(),
      serverConfigs: [
        {
          server: figmaServer,
          name: "Figma",
          remotes: figmaServer.remotes,
          identityMode: "user",
          agentAuthorization: "",
          headerValues: {},
        },
      ],
    });

    render(
      <TooltipProvider>
        <AddServerDialog
          servers={[figmaServer]}
          open
          onOpenChange={(open) => {
            mocks.onOpenChange(open);
          }}
        />
      </TooltipProvider>,
    );

    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Add to Project" }),
      ).toBeDefined(),
    );
    expect(
      (
        screen.getByRole("button", {
          name: "Add to Project",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(false);
    expect(screen.queryByRole("radio", { name: "User" })).toBeNull();
  });
});
