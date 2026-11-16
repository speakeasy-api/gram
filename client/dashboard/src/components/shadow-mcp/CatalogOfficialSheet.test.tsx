import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { testAccessSummary } from "./shadowMCPInventoryTestFixtures";

const mocks = vi.hoisted(() => ({
  save: vi.fn(),
  startInstall: vi.fn(),
  updateServerConfig: vi.fn(),
  mutateUpdate: vi.fn(),
  fetchQuery: vi.fn(),
  workflowServers: [] as unknown[],
  phase: "configure" as "configure" | "complete",
  statuses: [] as Array<Record<string, unknown>>,
}));

vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project-1", slug: "default" }),
}));
vi.mock("@/contexts/Sdk", () => ({ useSdkClient: () => ({}) }));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => true, isLoading: false }),
}));
vi.mock("@/lib/remote-identity", () => ({
  useAgentCredentialFields: () => ({ authorizationValue: "" }),
}));
vi.mock("@/pages/catalog/CatalogLogo", () => ({ CatalogLogo: () => null }));
vi.mock(
  "@/pages/mcp/x/tabs/settings/sections/authentication/CreationIdentityChoice",
  () => ({ CreationIdentityChoice: () => <div>identity choice</div> }),
);
vi.mock(
  "@/pages/mcp/x/tabs/settings/sections/authentication/AuthenticationSection",
  () => ({ AuthenticationSectionBody: () => <div>identity settings</div> }),
);
vi.mock(
  "@/pages/mcp/x/tabs/settings/sections/authentication/authTarget",
  () => ({ useMcpServerAuthTarget: () => ({ kind: "remote-mcp" }) }),
);
vi.mock("@gram/client/react-query/getMcpServer.js", () => ({
  useGetMcpServer: () => ({
    data: { id: "server-1", slug: "tracker", visibility: "disabled" },
  }),
  buildGetMcpServerQuery: () => ({ queryKey: ["server-1"] }),
}));
vi.mock("@gram/client/react-query/updateMcpServer.js", () => ({
  useUpdateMcpServerMutation: () => ({ mutateAsync: mocks.mutateUpdate }),
}));
vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({ fetchQuery: mocks.fetchQuery }),
}));
vi.mock("@/lib/mcp-server-visibility", () => ({
  invalidateMcpServerQueries: async () => {},
  mcpServerVisibilityUpdateForm: (_server: unknown, visibility: string) => ({
    visibility,
  }),
}));
vi.mock("@/pages/catalog/useRemoteMcpInstallWorkflow", () => ({
  useRemoteMcpInstallWorkflow: ({ servers }: { servers: unknown[] }) => {
    mocks.workflowServers = servers;
    if (mocks.phase === "complete") {
      return { phase: "complete", statuses: mocks.statuses, reset: () => {} };
    }
    return {
      phase: "configure",
      serverConfigs: [
        {
          name: "Tracker Cloud",
          identityMode: "user",
          headerValues: {},
        },
      ],
      updateServerConfig: mocks.updateServerConfig,
      setHeaderValue: vi.fn(),
      canInstall: true,
      startInstall: mocks.startInstall,
      reset: () => {},
    };
  },
}));
vi.mock("./useSaveShadowMCPOfficialServer", () => ({
  useSaveShadowMCPOfficialServer: () => ({
    save: mocks.save,
    isPending: false,
  }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: {
      details: {
        overview: {
          href: (param: string) => `/acme/projects/default/mcp/${param}`,
        },
        settings: {
          href: (param: string) =>
            `/acme/projects/default/mcp/${param}/settings`,
        },
      },
    },
  }),
}));

import { CatalogOfficialSheet } from "./CatalogOfficialSheet";

const entry = {
  registrySpecifier: "com.example/tracker",
  title: "Tracker Cloud",
  remotes: [
    { url: "https://tracker.example.com/sse", transportType: "sse" },
    {
      url: "https://tracker.example.com/mcp",
      transportType: "streamable-http",
    },
  ],
};

function server(): ShadowMCPInventoryServer {
  return {
    access: "blocked",
    accessSummary: testAccessSummary("blocked"),
    allowedPolicyIds: [],
    blockedPolicyIds: [],
    canonicalServerUrl: "https://tracker.example.com/mcp",
    firstSeen: new Date("2026-01-01T10:00:00Z"),
    lastSeen: new Date("2026-01-02T10:00:00Z"),
    observedUseCount: 13,
    requestCount: 0,
    serverName: "Tracker",
    serverSlug: "tracker-example-com-mcp-12345678",
    topUsers: [],
    urlHost: "tracker.example.com",
    userCount: 5,
  };
}

function renderSheet() {
  return render(
    <MemoryRouter>
      <CatalogOfficialSheet
        server={server()}
        entry={entry as never}
        probe={{
          outcome: "authentication_required",
          protectedResourceMetadataUrl:
            "https://tracker.example.com/.well-known/oauth-protected-resource",
          signInSetup: "manual",
        }}
        open
        onOpenChange={() => {}}
      />
    </MemoryRouter>,
  );
}

describe("CatalogOfficialSheet", () => {
  beforeEach(() => {
    mocks.phase = "configure";
    mocks.statuses = [];
    mocks.save.mockReset();
    mocks.save.mockResolvedValue(undefined);
    mocks.startInstall.mockReset();
    mocks.mutateUpdate.mockReset();
    mocks.fetchQuery.mockReset();
  });
  afterEach(cleanup);

  it("installs only the endpoint people use, then links it", async () => {
    const view = renderSheet();
    expect(screen.getByText("Add from the catalog")).toBeTruthy();
    expect(
      (mocks.workflowServers[0] as { remotes: Array<{ url: string }> }).remotes,
    ).toEqual([
      {
        url: "https://tracker.example.com/mcp",
        transportType: "streamable-http",
      },
    ]);

    fireEvent.click(screen.getByRole("button", { name: "Add and link" }));
    expect(mocks.startInstall).toHaveBeenCalled();

    mocks.phase = "complete";
    mocks.statuses = [
      {
        status: "completed",
        name: "Tracker Cloud",
        mcpServerId: "server-1",
        mcpServerParam: "tracker",
      },
    ];
    view.rerender(
      <MemoryRouter>
        <CatalogOfficialSheet
          server={server()}
          entry={entry as never}
          probe={null}
          open
          onOpenChange={() => {}}
        />
      </MemoryRouter>,
    );

    expect(await screen.findByText("Official server linked")).toBeTruthy();
    expect(mocks.save).toHaveBeenCalledWith({
      serverUrl: "https://tracker.example.com/mcp",
      mcpServerId: "server-1",
      allowDirectAccess: false,
    });
  });

  it("sets up sign-in in the sheet, then turns the server on and links it", async () => {
    mocks.phase = "complete";
    mocks.statuses = [
      {
        status: "failed",
        name: "Tracker Cloud",
        mcpServerId: "server-1",
        mcpServerParam: "tracker",
        setupRequired: "Automatic client registration is unavailable.",
      },
    ];
    mocks.fetchQuery.mockResolvedValue({
      id: "server-1",
      slug: "tracker",
      visibility: "disabled",
    });
    mocks.mutateUpdate.mockResolvedValue({
      id: "server-1",
      slug: "tracker",
      name: "Tracker Cloud",
      visibility: "private",
    });
    renderSheet();

    expect(await screen.findByText("Set up sign-in")).toBeTruthy();
    expect(screen.getByText("identity settings")).toBeTruthy();
    expect(mocks.save).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Turn on and link" }));
    expect(await screen.findByText("Official server linked")).toBeTruthy();
    expect(mocks.mutateUpdate).toHaveBeenCalledWith({
      request: { updateMcpServerForm: { visibility: "private" } },
    });
    expect(mocks.save).toHaveBeenCalledWith({
      serverUrl: "https://tracker.example.com/mcp",
      mcpServerId: "server-1",
      allowDirectAccess: false,
    });
  });
});
