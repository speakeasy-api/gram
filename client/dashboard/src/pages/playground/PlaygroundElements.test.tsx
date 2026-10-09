import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { useEffect } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { PlaygroundIssuerConnection } from "./usePlaygroundIssuerConnection";
import { PlaygroundElements } from "./PlaygroundElements";

const mocks = vi.hoisted(() => ({
  toolset: vi.fn(),
  connection: vi.fn(),
  chat: vi.fn(),
}));
vi.mock("@/hooks/toolTypes", () => ({ useToolset: mocks.toolset }));
vi.mock("@/hooks/useMissingEnvironmentVariables", () => ({
  useMissingRequiredEnvVars: () => 0,
}));
vi.mock("@gram/client/react-query/listEnvironments.js", () => ({
  useListEnvironments: () => ({}),
}));
vi.mock("@gram/client/react-query/getMcpMetadata.js", () => ({
  useGetMcpMetadata: () => ({}),
}));
vi.mock("@/routes", () => ({ useRoutes: vi.fn() }));
vi.mock("./usePlaygroundIssuerConnection", () => ({
  usePlaygroundIssuerConnection: mocks.connection,
}));
vi.mock("./PlaygroundChat", () => ({
  PlaygroundChat: mocks.chat,
}));

const connected: PlaygroundIssuerConnection = {
  mcpUrl: "https://platform.example/mcp/selected",
  isIssuerGated: true,
  accessToken: "token-S",
  connected: true,
  needsAuth: false,
  isLoading: false,
  isError: false,
  errorMessage: undefined,
  refetch: vi.fn<() => void>(),
  connect: vi.fn<() => void>(),
  canConnect: true,
};
function mount(toolsetSlug: string | null = "selected") {
  return render(
    <PlaygroundElements
      toolsetSlug={toolsetSlug}
      environmentSlug={null}
      model="test"
    />,
  );
}
beforeEach(() => {
  vi.clearAllMocks();
  mocks.toolset.mockReturnValue({
    data: { id: "T", slug: "selected", name: "Provider" },
  });
  mocks.connection.mockReturnValue(connected);
  mocks.chat.mockReturnValue(<div>Chat ready</div>);
});
afterEach(cleanup);

describe("PlaygroundElements connection states", () => {
  it("asks for a selection only when none is selected", () => {
    mount(null);
    expect(screen.getByRole("status").textContent).toContain(
      "Select an MCP server",
    );
    expect(mocks.chat).not.toHaveBeenCalled();
  });

  it.each(["toolset", "lookup", "mint", "probe"])(
    "shows loading during %s, not a selection prompt",
    (stage) => {
      if (stage === "toolset")
        mocks.toolset.mockReturnValue({ isLoading: true });
      mocks.connection.mockReturnValue({
        ...connected,
        isLoading: stage !== "toolset",
        connected: false,
        ...(stage === "mint" ? { accessToken: undefined } : {}),
      });
      mount();
      expect(screen.getByRole("status").textContent).toContain("Connecting");
      expect(screen.queryByText(/Select an MCP server/)).toBeNull();
      expect(mocks.chat).not.toHaveBeenCalled();
    },
  );

  it("blocks chat when the toolset lookup fails", () => {
    mocks.toolset.mockReturnValue({ isError: true });
    mount();
    expect(screen.getByRole("alert").textContent).toContain("Unable to load");
    expect(mocks.chat).not.toHaveBeenCalled();
  });

  it.each([
    "Unable to load the selected MCP server. Try again.",
    "Unable to connect to the selected MCP server. Try again.",
    "Unable to create a session for the selected MCP server. Try again.",
    "The selected MCP server has no platform address available for the playground.",
  ])("blocks chat and displays %s", (errorMessage) => {
    mocks.connection.mockReturnValue({
      ...connected,
      isError: true,
      errorMessage,
    });
    mount();
    expect(screen.getByRole("alert").textContent).toContain(errorMessage);
    expect(mocks.chat).not.toHaveBeenCalled();
  });

  it("shows login required for a 401", () => {
    mocks.connection.mockReturnValue({
      ...connected,
      connected: false,
      needsAuth: true,
    });
    mount();
    expect(screen.getByText("Login Required")).toBeTruthy();
    expect(mocks.chat).not.toHaveBeenCalled();
  });

  it("does not render chat before a successful issuer probe", () => {
    mocks.connection.mockReturnValue({ ...connected, connected: false });
    mount();
    expect(mocks.chat).not.toHaveBeenCalled();
  });

  it("passes the selected server URL and token to chat after connection", () => {
    mount();
    expect(screen.getByText("Chat ready")).toBeTruthy();
    expect(mocks.chat.mock.calls[0]?.[0]).toMatchObject({
      mcpUrl: connected.mcpUrl,
      gatewayToken: "token-S",
    });
  });
});

it.each(["toolset", "connection"])(
  "retries only the failed %s stage and blocks chat while retrying",
  (stage) => {
    const retryToolset = vi.fn();
    mocks.toolset.mockReturnValue({
      isError: stage === "toolset",
      refetch: retryToolset,
    });
    mocks.connection.mockReturnValue({
      ...connected,
      isError: stage === "connection",
      errorMessage: "Temporary failure",
    });
    const { rerender } = mount();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(retryToolset).toHaveBeenCalledTimes(stage === "toolset" ? 1 : 0);
    expect(connected.refetch).toHaveBeenCalledTimes(
      stage === "connection" ? 1 : 0,
    );
    mocks.toolset.mockReturnValue({
      isError: stage === "toolset",
      isFetching: stage === "toolset",
    });
    mocks.connection.mockReturnValue({
      ...connected,
      isLoading: stage === "connection",
      isError: stage === "connection",
    });
    rerender(
      <PlaygroundElements
        toolsetSlug="selected"
        environmentSlug={null}
        model="test"
      />,
    );
    expect(screen.getByRole("status").textContent).toContain("Connecting");
    expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
    expect(mocks.chat).not.toHaveBeenCalled();
    mocks.toolset.mockReturnValue({ data: { id: "T", slug: "selected" } });
    mocks.connection.mockReturnValue(connected);
    rerender(
      <PlaygroundElements
        toolsetSlug="selected"
        environmentSlug={null}
        model="test"
      />,
    );
    expect(screen.getByText("Chat ready")).toBeTruthy();
  },
);

it("preserves mounted chat when a healthy cached connection refreshes", () => {
  const mounted = vi.fn();
  const unmounted = vi.fn();
  mocks.chat.mockImplementation(function Chat() {
    useEffect(() => {
      mounted();
      return unmounted;
    }, []);
    return <div>Chat ready</div>;
  });
  const { rerender } = mount();
  mocks.connection.mockReturnValue({ ...connected });
  rerender(
    <PlaygroundElements
      toolsetSlug="selected"
      environmentSlug={null}
      model="test"
    />,
  );
  expect(mounted).toHaveBeenCalledTimes(1);
  expect(unmounted).not.toHaveBeenCalled();
  expect(screen.getByText("Chat ready")).toBeTruthy();
});
