import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  toolset: undefined as unknown,
  metadataArgs: [] as unknown[],
}));

vi.mock("@/hooks/toolTypes", () => ({
  useToolset: () => ({ data: mocks.toolset }),
}));
vi.mock("@gram/client/react-query/listEnvironments.js", () => ({
  useListEnvironments: () => ({ data: { environments: [] } }),
}));
vi.mock("@gram/client/react-query/getMcpMetadata.js", () => ({
  useGetMcpMetadata: (args: unknown) => {
    mocks.metadataArgs.push(args);
    return { data: undefined, isLoading: false };
  },
}));
vi.mock("@/pages/mcp/useEnvironmentVariables", () => ({
  useEnvironmentVariables: () => [],
}));
vi.mock("@/components/public-mcp-warning-dialog", () => ({
  PublicMcpWarningDialog: ({
    isOpen,
    onConfirm,
  }: {
    isOpen: boolean;
    onConfirm: () => void;
  }) => (isOpen ? <button onClick={onConfirm}>Confirm public</button> : null),
}));
vi.mock("@/components/server-enable-dialog", () => ({
  ServerEnableDialog: ({
    isOpen,
    onConfirm,
  }: {
    isOpen: boolean;
    onConfirm: () => void;
  }) => (isOpen ? <button onClick={onConfirm}>Confirm enable</button> : null),
}));

import { useHostedVisibilityGuard } from "./useHostedVisibilityGuard";

const apply = vi.fn();
let request: (next: McpServer["visibility"]) => void = () => {};

function Harness({ server }: { server: McpServer }) {
  const guard = useHostedVisibilityGuard({
    server,
    hosted: true,
    apply: (next) => {
      apply(next);
    },
    isApplying: false,
  });
  request = guard.request;
  return <>{guard.dialogs}</>;
}

const canonical = {
  id: "toolset-1",
  toolsetId: "toolset-1",
  toolset: { slug: "my-tools" },
  visibility: "private",
} as McpServer;

beforeEach(() => {
  mocks.toolset = { slug: "my-tools", mcpIsPublic: false };
  mocks.metadataArgs = [];
});
afterEach(() => {
  cleanup();
  apply.mockClear();
});

describe("useHostedVisibilityGuard", () => {
  it("warns before going public, then applies", () => {
    render(<Harness server={canonical} />);
    act(() => request("public"));
    expect(apply).not.toHaveBeenCalled();
    act(() => screen.getByText("Confirm public").click());
    expect(apply).toHaveBeenCalledWith("public");
  });

  it("chains the public warning into the enable dialog for a disabled server", () => {
    render(<Harness server={{ ...canonical, visibility: "disabled" }} />);
    act(() => request("public"));
    act(() => screen.getByText("Confirm public").click());
    expect(apply).not.toHaveBeenCalled();
    act(() => screen.getByText("Confirm enable").click());
    expect(apply).toHaveBeenCalledWith("public");
  });

  it("blocks a public flip while external OAuth is attached", () => {
    mocks.toolset = {
      slug: "my-tools",
      mcpIsPublic: true,
      externalOauthServer: { slug: "oauth" },
    };
    render(<Harness server={{ ...canonical, visibility: "public" }} />);
    act(() => request("private"));
    expect(screen.getByText("Remove external OAuth first")).toBeDefined();
    expect(apply).not.toHaveBeenCalled();
  });

  it("reads a non-canonical host's metadata by its own id", () => {
    render(
      <Harness
        server={{ ...canonical, id: "host-1", toolsetId: "toolset-1" }}
      />,
    );
    expect(mocks.metadataArgs.at(-1)).toEqual({ mcpServerId: "host-1" });
  });
});
