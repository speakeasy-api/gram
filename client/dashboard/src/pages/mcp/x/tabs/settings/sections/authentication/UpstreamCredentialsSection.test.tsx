import { TooltipProvider } from "@/components/ui/Tooltip";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { UpstreamCredentialsSection } from "./UpstreamCredentialsSection";

const mocks = vi.hoisted(() => ({
  probe: "idle",
  probed: vi.fn(),
  admin: true,
  authentication: {
    status: "no_auth_required",
    requiresUpstreamAuth: false,
    clients: [] as unknown[],
  },
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => mocks.admin }),
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
    data: mocks.authentication,
  }),
}));

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
  mocks.admin = true;
  mocks.authentication = {
    status: "no_auth_required",
    requiresUpstreamAuth: false,
    clients: [],
  };
});

describe("UpstreamCredentialsSection", () => {
  it("says what probing a remote upstream found", () => {
    mocks.probe = "authentication-required";
    renderSection({ remoteMcpServerId: "remote-1" });

    expect(mocks.probed).toHaveBeenCalledWith("remote-1", true);
    expect(screen.getByText(/it asks for sign-in/)).toBeTruthy();
  });

  it.each([
    ["configured", "Credentials set up"],
    ["setup_incomplete", "Authentication setup incomplete"],
    ["no_auth_required", "No authentication required"],
  ])("states the %s status without a control to change it", (status, label) => {
    mocks.authentication = { ...mocks.authentication, status };
    renderSection({ remoteMcpServerId: "remote-1" });

    expect(screen.getByText(label)).toBeTruthy();
    expect(screen.queryByRole("radiogroup")).toBeNull();
    expect(screen.queryByText(/Needs none/i)).toBeNull();
    expect(screen.getByText("Add credential")).toBeTruthy();
  });

  it("flags setup when the upstream asks for sign-in and no credential is on", () => {
    mocks.probe = "authentication-required";
    renderSection({ remoteMcpServerId: "remote-1" });

    expect(screen.getByText("Authentication setup incomplete")).toBeTruthy();
    expect(screen.getByText(/it asks for sign-in/)).toBeTruthy();
  });

  it("leaves the derived status alone for people who cannot change it", () => {
    mocks.admin = false;
    mocks.probe = "authentication-required";
    renderSection({ remoteMcpServerId: "remote-1" });

    expect(mocks.probed).toHaveBeenCalledWith("remote-1", false);
    expect(screen.getByText("No authentication required")).toBeTruthy();
    expect(screen.queryByText("Add credential")).toBeNull();
  });

  it("never reads an unknown status as needing no authentication", () => {
    mocks.authentication = { ...mocks.authentication, status: "future" };
    renderSection({ remoteMcpServerId: "remote-1" });

    expect(screen.getByText("Status unavailable")).toBeTruthy();
    expect(screen.queryByText("No authentication required")).toBeNull();
  });

  it("does not probe a server with no remote upstream", () => {
    renderSection({ tunneledMcpServerId: "tunnel-1" });

    expect(mocks.probed).toHaveBeenCalledWith("", false);
    expect(screen.queryByText(/Speakeasy checked the upstream/)).toBeNull();
  });
});
