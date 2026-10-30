import { TooltipProvider } from "@/components/ui/Tooltip";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { UpstreamCredentialsSection } from "./UpstreamCredentialsSection";

const mocks = vi.hoisted(() => ({
  probe: "idle",
  probed: vi.fn(),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => true }),
}));

vi.mock("@/lib/remote-identity", () => ({
  useUpstreamProbe: (remoteMcpServerId: string, enabled: boolean) => {
    mocks.probed(remoteMcpServerId, enabled);
    return enabled ? mocks.probe : "idle";
  },
}));

vi.mock("@gram/client/react-query/mcpServerAuthentication.js", () => ({
  useMcpServerAuthentication: () => ({
    isLoading: false,
    isError: false,
    data: { status: "undecided", requiresUpstreamAuth: undefined, clients: [] },
  }),
}));

vi.mock(
  "@gram/client/react-query/setMcpServerUpstreamAuthRequirement.js",
  () => ({
    useSetMcpServerUpstreamAuthRequirementMutation: () => ({
      mutate: vi.fn(),
      isPending: false,
    }),
  }),
);

vi.mock("@gram/client/react-query/updateAuthClient.js", () => ({
  useUpdateAuthClientMutation: () => ({ mutate: vi.fn(), isPending: false }),
}));

function renderSection(server: Partial<McpServer>) {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <TooltipProvider>
        <UpstreamCredentialsSection
          mcpServer={
            { id: "server-1", projectId: "project-1", ...server } as McpServer
          }
        />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  mocks.probe = "idle";
});

describe("UpstreamCredentialsSection", () => {
  it("says what probing a remote upstream found", () => {
    mocks.probe = "authentication-required";
    renderSection({ remoteMcpServerId: "remote-1" });

    expect(mocks.probed).toHaveBeenCalledWith("remote-1", true);
    expect(screen.getByText(/it asks for sign-in/)).toBeTruthy();
  });

  it("does not probe a server with no remote upstream", () => {
    renderSection({ tunneledMcpServerId: "tunnel-1" });

    expect(mocks.probed).toHaveBeenCalledWith("", false);
    expect(screen.queryByText(/Speakeasy checked the upstream/)).toBeNull();
  });
});
