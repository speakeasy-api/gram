import { TooltipProvider } from "@/components/ui/Tooltip";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { catalogPresetState } from "@/pages/security/server-guardrails/server-guardrail-policy";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AddServerDialog, GuardrailOutcomeNotice } from "./AddServerDialog";

const mocks = vi.hoisted(() => ({
  getServerDetails: vi.fn(),
  hasScope: vi.fn(),
  onOpenChange: vi.fn(),
  reset: vi.fn(),
  startInstall: vi.fn(),
  continueToGuardrails: vi.fn(),
  skip: vi.fn(),
  installWithGuardrail: vi.fn(),
  updateServerConfig: vi.fn(),
  workflow: vi.fn(),
}));

vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project-1", slug: "default" }),
  useOrganization: () => ({ id: "org-1", slug: "acme" }),
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
      expect(
        screen.getByRole("radio", { name: /User Identity/ }),
      ).toBeDefined(),
    );
    expect(
      (
        screen.getByRole("radio", {
          name: /User Identity/,
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    expect(
      screen.getByText(/User Identity creates an identity provider/i),
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
      expect(
        screen.getByRole("radio", { name: /User Identity/ }),
      ).toBeDefined(),
    );
    expect(
      (
        screen.getByRole("radio", {
          name: /User Identity/,
        }) as HTMLButtonElement
      ).disabled,
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
});

describe("AddServerDialog guardrails", () => {
  it("continues to the guardrails step instead of installing when offered", async () => {
    mocks.hasScope.mockReturnValue(true);
    const configure = mocks.workflow();
    mocks.workflow.mockReturnValue({
      ...configure,
      continueToGuardrails: mocks.continueToGuardrails,
    });
    renderDialog();

    fireEvent.click(await screen.findByRole("button", { name: "Continue" }));

    expect(mocks.continueToGuardrails).toHaveBeenCalledWith({
      configureSkipped: false,
    });
    expect(mocks.startInstall).not.toHaveBeenCalled();
  });

  it("offers to skip the guardrail or install with it", async () => {
    mocks.workflow.mockReturnValue({
      phase: "guardrails",
      projectSlug: "default",
      guardrail: catalogPresetState([
        { name: "delete_issue", destructive: true },
      ]),
      updateGuardrail: vi.fn(),
      serverNames: ["Test Server"],
      installWithGuardrail: mocks.installWithGuardrail,
      skip: mocks.skip,
      goBack: vi.fn(),
      isServerAlreadyInstalled: () => false,
      reset: mocks.reset,
    });
    renderDialog();

    expect(
      await screen.findByText("Create a risk policy for Test Server"),
    ).toBeDefined();
    fireEvent.click(screen.getByRole("button", { name: "Skip for now" }));
    expect(mocks.skip).toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Add to Project" }));
    expect(mocks.installWithGuardrail).toHaveBeenCalled();
  });

  it("says plainly when the guardrail could not be created", () => {
    render(
      <GuardrailOutcomeNotice
        outcome={{
          status: "failed",
          name: "Test Server guardrail",
          error: "policy limit reached",
        }}
      />,
    );

    const alert = screen.getByRole("alert");
    expect(alert.textContent).toContain("Guardrail was not created");
    expect(alert.textContent).toContain("policy limit reached");
  });

  it("confirms a created guardrail", () => {
    render(
      <GuardrailOutcomeNotice
        outcome={{ status: "created", name: "Test Server guardrail" }}
      />,
    );

    expect(screen.getByText("Guardrail created")).toBeDefined();
  });
});
