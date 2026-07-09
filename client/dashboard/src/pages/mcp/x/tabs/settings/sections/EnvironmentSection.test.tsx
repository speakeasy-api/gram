import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  updateMcpServer: vi.fn().mockResolvedValue(undefined),
  listEnvironments: vi.fn(),
}));

vi.mock("@gram/client/react-query/listEnvironments.js", () => ({
  useListEnvironments: () => mocks.listEnvironments(),
}));

vi.mock("@gram/client/react-query/updateMcpServer.js", () => ({
  useUpdateMcpServerMutation: (options: {
    onSuccess?: () => void;
    onError?: (e: Error) => void;
  }) => ({
    mutate: (vars: unknown) => {
      mocks.updateMcpServer(vars).then(
        () => options.onSuccess?.(),
        (e: Error) => options.onError?.(e),
      );
    },
    isPending: false,
  }),
}));

vi.mock("@gram/client/react-query/getMcpServer.js", () => ({
  invalidateAllGetMcpServer: vi.fn(),
}));

vi.mock("@gram/client/react-query/mcpServers.js", () => ({
  invalidateAllMcpServers: vi.fn(),
}));

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("@speakeasy-api/moonshine", () => {
  const Button = ({
    children,
    onClick,
    disabled,
  }: {
    children: ReactNode;
    onClick?: () => void;
    disabled?: boolean;
  }) => (
    <button type="button" onClick={onClick} disabled={disabled}>
      {children}
    </button>
  );
  Button.Text = ({ children }: { children: ReactNode }) => (
    <span>{children}</span>
  );
  Button.LeftIcon = ({ children }: { children: ReactNode }) => (
    <span>{children}</span>
  );

  return { Button };
});

vi.mock("sonner", () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
  },
}));

import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { EnvironmentSection } from "./EnvironmentSection";

const remoteServer: McpServer = {
  id: "mcp-server-1",
  projectId: "project-1",
  name: "Remote server",
  visibility: "public",
  remoteMcpServerId: "remote-backend-1",
  createdAt: new Date(),
  updatedAt: new Date(),
};

const toolsetServer: McpServer = {
  id: "mcp-server-2",
  projectId: "project-1",
  name: "Toolset server",
  visibility: "public",
  toolsetId: "toolset-1",
  createdAt: new Date(),
  updatedAt: new Date(),
};

function renderSection(server: McpServer) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <EnvironmentSection mcpServer={server} />
    </QueryClientProvider>,
  );
}

describe("EnvironmentSection", () => {
  beforeEach(() => {
    mocks.listEnvironments.mockReturnValue({
      data: {
        environments: [
          {
            id: "env-1",
            name: "Production",
            slug: "production",
            entries: [],
            createdAt: new Date(),
            updatedAt: new Date(),
          },
        ],
      },
      isLoading: false,
    });
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("is hidden for non-remote-backed servers", () => {
    const { container } = renderSection(toolsetServer);
    expect(container.firstChild).toBeNull();
  });

  it("attaches an environment via updateMcpServer", async () => {
    renderSection(remoteServer);

    fireEvent.click(screen.getByRole("combobox"));
    fireEvent.click(screen.getByRole("option", { name: "Production" }));
    fireEvent.click(screen.getByRole("button", { name: /save/i }));

    await waitFor(() => {
      expect(mocks.updateMcpServer).toHaveBeenCalledWith({
        request: {
          updateMcpServerForm: expect.objectContaining({
            id: "mcp-server-1",
            environmentId: "env-1",
          }),
        },
      });
    });
  });

  it("detaches an environment when None is selected", async () => {
    renderSection({ ...remoteServer, environmentId: "env-1" });

    fireEvent.click(screen.getByRole("combobox"));
    fireEvent.click(screen.getByRole("option", { name: "None" }));
    fireEvent.click(screen.getByRole("button", { name: /save/i }));

    await waitFor(() => {
      expect(mocks.updateMcpServer).toHaveBeenCalledWith({
        request: {
          updateMcpServerForm: expect.objectContaining({
            id: "mcp-server-1",
            environmentId: undefined,
          }),
        },
      });
    });
  });
});
