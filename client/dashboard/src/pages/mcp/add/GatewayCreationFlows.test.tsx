import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import CreateRemoteMcp from "@/pages/sources/remote-mcp/CreateRemoteMcp";
import CreateTunneledMcp from "@/pages/sources/tunneled-mcp/CreateTunneledMcp";
import CreateFromSource from "./CreateFromSource";
const state = vi.hoisted(() => ({
  flow: {
    gatewayId: "gateway" as string | null,
    createdServerId: null as string | null,
    isAttaching: false,
    attachmentError: null as string | null,
    complete: vi.fn(),
    cancel: vi.fn(),
    retry: vi.fn(),
  },
  create: vi.fn(),
  createToolset: vi.fn(),
  createServer: vi.fn(),
  listServers: vi.fn(),
  navigate: vi.fn(),
  // Route helper `goTo` and the router's own navigate are separate calls; the
  // setup-required path uses the latter to land on Settings > Identity.
  routerNavigate: vi.fn(),
}));
vi.mock("@/pages/mcp/gateway/useGatewayCreation", async (importOriginal) => ({
  ...(await importOriginal<
    typeof import("@/pages/mcp/gateway/useGatewayCreation")
  >()),
  useGatewayCreation: () => state.flow,
}));
vi.mock("@/components/page-templates", () => ({
  FormPage: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: {
      add: { goTo: state.navigate },
      x: {
        overview: { goTo: state.navigate },
        settings: { href: () => "/mcp/x/server/settings" },
      },
      details: { tools: { goTo: state.navigate } },
    },
  }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => true, isLoading: false }),
}));
vi.mock("@/contexts/Auth", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/contexts/Auth")>()),
  useIsSpeakeasyStaff: () => true,
  useProject: () => ({ id: "project-1" }),
  useIsPlatformAdmin: () => false,
}));
vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ isFeatureEnabled: () => true }),
}));
vi.mock("@/hooks/useEffectiveUserSessionIssuers", () => ({
  useEffectiveUserSessionIssuers: () => ({
    issuers: [],
    organizationIssuers: [],
    isLoading: false,
    isError: false,
  }),
}));
vi.mock("@/pages/sources/remote-mcp/hooks", () => ({
  useCreateRemoteMcpSource: () => ({ mutateAsync: state.create }),
}));
vi.mock("@/pages/sources/unproxied-mcp/hooks", () => ({
  useCreateUnproxiedMcpSource: () => ({ mutateAsync: state.create }),
}));
vi.mock("@/pages/sources/tunneled-mcp/hooks", () => ({
  useCreateTunneledMcpSource: () => ({ mutateAsync: state.create }),
}));
vi.mock("@/pages/sources/remote-mcp/useVerifyRemoteMcpUrl", () => ({
  useVerifyRemoteMcpUrl: () => ({
    result: { verified: true },
    trigger: vi.fn(),
  }),
}));
vi.mock("@/pages/sources/remote-mcp/VerifyRemoteMcpUrlButton", () => ({
  VerifyRemoteMcpUrlAlert: () => null,
}));
vi.mock("@/pages/sources/tunneled-mcp/TunneledMcpSetupTabs", () => ({
  TunneledMcpSetupTabs: () => null,
}));
vi.mock("@/components/code", () => ({
  CodeBlock: ({ children }: { children: React.ReactNode }) => (
    <pre>{children}</pre>
  ),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    toolsets: { create: state.createToolset },
    mcpServers: { create: state.createServer, list: state.listServers },
  }),
}));
vi.mock("@/components/side-panel/side-panel-context", () => ({
  useSidePanel: () => ({ openPanel: vi.fn() }),
}));
vi.mock("@/components/sources/source-list", () => ({
  useProjectSources: () => ({
    sources: [
      {
        key: "source",
        name: "Source",
        kind: "openapi",
        document: { id: "document" },
      },
    ],
  }),
  sourceAssetId: () => "document",
}));
vi.mock("@/components/sources/source-grid", () => ({
  SourceCard: ({ onSelect }: { onSelect: () => void }) => (
    <button type="button" onClick={onSelect}>
      Select source
    </button>
  ),
}));
vi.mock("@/hooks/toolTypes", () => ({
  useListTools: () => ({ data: { tools: [] } }),
}));
vi.mock("react-router", () => ({
  useSearchParams: () => [new URLSearchParams()],
  Navigate: () => null,
  useNavigate: () => state.routerNavigate,
}));
beforeEach(() => {
  vi.clearAllMocks();
  Object.assign(state.flow, {
    gatewayId: "gateway",
    createdServerId: null,
    attachmentError: null,
    isAttaching: false,
  });
  state.flow.cancel.mockReturnValue(true);
  state.flow.complete.mockResolvedValue(undefined);
  state.flow.retry.mockResolvedValue(undefined);
  state.create.mockResolvedValue({
    mcpServer: { id: "server", slug: "server" },
    identityConfiguration: { status: "configured" },
    tunneledMcpServer: { id: "tunnel", slug: "tunnel" },
    tunnelKey: "test-key",
  });
  state.createToolset.mockResolvedValue({
    id: "toolset",
    slug: "toolset",
    name: "Source",
  });
  state.createServer.mockResolvedValue({ id: "server" });
  state.listServers.mockResolvedValue({ mcpServers: [] });
});
afterEach(cleanup);
const cases = [
  {
    name: "remote",
    Component: CreateRemoteMcp,
    fill: () =>
      fireEvent.change(screen.getByLabelText("MCP server URL"), {
        target: { value: "https://example.com/mcp" },
      }),
    button: "Save",
  },
  {
    name: "tunneled",
    Component: CreateTunneledMcp,
    fill: () =>
      fireEvent.change(screen.getByLabelText("Display name"), {
        target: { value: "Server" },
      }),
    button: "Add server",
  },
  {
    name: "source",
    Component: CreateFromSource,
    fill: () => fireEvent.click(screen.getByText("Select source")),
    button: "Create",
  },
];
for (const { name, Component, fill, button } of cases) {
  describe(`${name} gateway creation`, () => {
    it("attaches the created MCP server instead of navigating standalone", async () => {
      render(<Component />);
      fill();
      fireEvent.click(screen.getByRole("button", { name: button }));
      if (name === "tunneled") {
        expect(await screen.findByText("test-key")).toBeTruthy();
        expect(state.flow.complete).not.toHaveBeenCalled();
        expect(state.navigate).not.toHaveBeenCalled();
        expect(
          screen.queryByRole("button", { name: "View source" }),
        ).toBeNull();
        fireEvent.click(screen.getByRole("button", { name: "Add to gateway" }));
      }
      await waitFor(() =>
        expect(state.flow.complete).toHaveBeenCalledWith("server"),
      );
      expect(state.navigate).not.toHaveBeenCalled();
      if (name === "source")
        expect(state.createServer).toHaveBeenCalledWith({
          createMcpServerForm: {
            name: "Source",
            toolsetId: "toolset",
            visibility: "private",
          },
        });
    });
    it("disables cancellation while attachment is in flight", () => {
      state.flow.isAttaching = true;
      render(<Component />);
      expect(
        (screen.getByRole("button", { name: "Cancel" }) as HTMLButtonElement)
          .disabled,
      ).toBe(true);
    });
    it("returns to the gateway on cancel without creating anything", async () => {
      render(<Component />);
      fill();
      await act(async () => {
        fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
      });
      expect(state.flow.cancel).toHaveBeenCalled();
      expect(state.navigate).not.toHaveBeenCalled();
      expect(state.create).not.toHaveBeenCalled();
      expect(state.createToolset).not.toHaveBeenCalled();
      expect(state.createServer).not.toHaveBeenCalled();
      expect(state.flow.complete).not.toHaveBeenCalled();
    });
    it("locks creation and shows attachment recovery after failure", () => {
      state.flow.createdServerId = "server";
      state.flow.attachmentError = "Attachment failed";
      render(<Component />);
      const action = screen.getByRole("button", {
        name: button,
      }) as HTMLButtonElement;
      expect(action.disabled || action.closest("fieldset")?.disabled).toBe(
        true,
      );
      expect(screen.getByRole("alert").textContent).toContain(
        "Attachment failed",
      );
      fireEvent.click(
        screen.getByRole("button", { name: "Retry adding to gateway" }),
      );
      expect(state.flow.retry).toHaveBeenCalledOnce();
      fireEvent.submit(action.closest("form")!);
      expect(state.create).not.toHaveBeenCalled();
      expect(state.createToolset).not.toHaveBeenCalled();
    });
    it("keeps standalone creation behavior", async () => {
      state.flow.gatewayId = null;
      render(<Component />);
      fill();
      fireEvent.click(screen.getByRole("button", { name: button }));
      if (name === "tunneled") {
        await screen.findByText("Open MCP server");
      } else {
        await waitFor(() => expect(state.navigate).toHaveBeenCalled());
      }
      expect(state.flow.complete).not.toHaveBeenCalled();
      expect(state.createServer).not.toHaveBeenCalled();
    });
    it("keeps standalone cancellation without creating anything", async () => {
      state.flow.gatewayId = null;
      state.flow.cancel.mockReturnValue(false);
      render(<Component />);
      fill();
      await act(async () => {
        fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
      });
      expect(state.navigate).toHaveBeenCalled();
      expect(state.create).not.toHaveBeenCalled();
      expect(state.createToolset).not.toHaveBeenCalled();
      expect(state.createServer).not.toHaveBeenCalled();
      expect(state.flow.complete).not.toHaveBeenCalled();
    });
  });
}
it("disables direct connections only in gateway context", () => {
  const view = render(<CreateRemoteMcp />);
  expect(
    screen
      .getByRole("radio", { name: /Clients connect directly/ })
      .hasAttribute("disabled"),
  ).toBe(true);
  state.flow.gatewayId = null;
  view.rerender(<CreateRemoteMcp />);
  expect(
    screen
      .getByRole("radio", { name: /Clients connect directly/ })
      .hasAttribute("disabled"),
  ).toBe(false);
});

it("can cancel from the tunnel key screen without attaching", async () => {
  render(<CreateTunneledMcp />);
  fireEvent.change(screen.getByLabelText("Display name"), {
    target: { value: "Server" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Add server" }));
  await screen.findByText("test-key");
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  expect(state.flow.cancel).toHaveBeenCalledOnce();
  expect(state.flow.complete).not.toHaveBeenCalled();
});
it("keeps the tunnel key and retry available after attachment failure", async () => {
  const view = render(<CreateTunneledMcp />);
  fireEvent.change(screen.getByLabelText("Display name"), {
    target: { value: "Server" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Add server" }));
  await screen.findByText("test-key");
  state.flow.complete.mockRejectedValueOnce(new Error("Attachment failed"));
  fireEvent.click(screen.getByRole("button", { name: "Add to gateway" }));
  await waitFor(() =>
    expect(state.flow.complete).toHaveBeenCalledWith("server"),
  );
  state.flow.attachmentError = "Attachment failed";
  state.flow.createdServerId = "server";
  state.flow.isAttaching = true;
  view.rerender(<CreateTunneledMcp />);
  expect(
    (
      screen.getByRole("button", {
        name: "Add to gateway",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
  state.flow.isAttaching = false;
  view.rerender(<CreateTunneledMcp />);
  expect(screen.getByText("test-key")).toBeTruthy();
  fireEvent.click(
    screen.getByRole("button", { name: "Retry adding to gateway" }),
  );
  expect(state.flow.retry).toHaveBeenCalledOnce();
  expect(state.create).toHaveBeenCalledOnce();
});

describe("source wrapper recovery", () => {
  async function failWrapper() {
    state.createServer.mockRejectedValueOnce(new Error("Wrapper failed"));
    render(<CreateFromSource />);
    fireEvent.click(screen.getByText("Select source"));
    fireEvent.click(screen.getByRole("button", { name: "Create" }));
    await screen.findByText("Wrapper failed");
  }
  it("fails closed when the uncertain wrapper is absent from read results", async () => {
    await failWrapper();
    fireEvent.click(screen.getByRole("button", { name: /Create|Retry/ }));
    await screen.findByText(/manually locate and attach/);
    expect(state.createToolset).toHaveBeenCalledOnce();
    expect(state.createServer).toHaveBeenCalledOnce();
    expect(state.flow.complete).not.toHaveBeenCalled();
    expect(state.listServers).toHaveBeenCalledWith({ toolsetId: "toolset" });
  });
  it("recovers a lost wrapper response by exact toolset before retrying writes", async () => {
    await failWrapper();
    state.listServers.mockResolvedValue({ mcpServers: [{ id: "recovered" }] });
    fireEvent.click(screen.getByRole("button", { name: /Create|Retry/ }));
    await waitFor(() =>
      expect(state.flow.complete).toHaveBeenCalledWith("recovered"),
    );
    expect(state.createToolset).toHaveBeenCalledOnce();
    expect(state.createServer).toHaveBeenCalledOnce();
    expect(state.listServers).toHaveBeenCalledWith({ toolsetId: "toolset" });
  });
  it.each(["read failure", "ambiguous wrappers"])(
    "prevents writes on %s and remains retryable",
    async (failure) => {
      await failWrapper();
      if (failure === "read failure")
        state.listServers.mockRejectedValueOnce(new Error("Read failed"));
      else
        state.listServers.mockResolvedValueOnce({
          mcpServers: [{ id: "one" }, { id: "two" }],
        });
      fireEvent.click(screen.getByRole("button", { name: /Create|Retry/ }));
      await screen.findByText(
        failure === "read failure" ? "Read failed" : /Multiple servers/,
      );
      expect(state.createToolset).toHaveBeenCalledOnce();
      expect(state.createServer).toHaveBeenCalledOnce();
      expect(state.flow.complete).not.toHaveBeenCalled();
      state.listServers.mockResolvedValue({ mcpServers: [{ id: "server" }] });
      fireEvent.click(screen.getByRole("button", { name: /Create|Retry/ }));
      await waitFor(() =>
        expect(state.flow.complete).toHaveBeenCalledWith("server"),
      );
      expect(state.createToolset).toHaveBeenCalledOnce();
    },
  );
});
