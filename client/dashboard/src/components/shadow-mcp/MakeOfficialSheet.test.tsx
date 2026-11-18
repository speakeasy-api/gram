import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { testAccessSummary } from "./shadowMCPInventoryTestFixtures";

const mocks = vi.hoisted(() => ({
  create: vi.fn(),
  save: vi.fn(),
}));

vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project-1", slug: "default" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => true, isLoading: false }),
}));
vi.mock("@/hooks/useEffectiveUserSessionIssuers", () => ({
  useEffectiveUserSessionIssuers: () => ({
    organizationIssuers: [{ id: "issuer-1", name: "Acme" }],
    isLoading: false,
    isError: false,
  }),
}));
vi.mock("@/lib/remote-identity", () => ({
  useAgentCredentialFields: () => ({ authorizationValue: "" }),
}));
vi.mock(
  "@/pages/mcp/x/tabs/settings/sections/authentication/CreationIdentityChoice",
  () => ({
    CreationIdentityChoice: () => <div>identity choice</div>,
  }),
);
vi.mock("@/components/user-session-issuer-select", () => ({
  UserSessionIssuerSelect: () => <div>issuer select</div>,
}));
vi.mock("@/pages/sources/remote-mcp/hooks", () => ({
  useCreateRemoteMcpSource: () => ({ mutateAsync: mocks.create }),
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

import { MakeOfficialSheet } from "./MakeOfficialSheet";

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

describe("MakeOfficialSheet", () => {
  beforeEach(() => {
    mocks.create.mockReset();
    mocks.save.mockReset();
    mocks.save.mockResolvedValue(undefined);
  });
  afterEach(cleanup);

  it("makes the server official and links it in one step", async () => {
    mocks.create.mockResolvedValue({
      mcpServer: { id: "server-1", slug: "tracker", name: "Tracker" },
      identityConfiguration: { status: "configured" },
    });
    render(
      <MemoryRouter>
        <MakeOfficialSheet
          server={server()}
          probe={{
            outcome: "authentication_required",
            protectedResourceMetadataUrl:
              "https://tracker.example.com/.well-known/oauth-protected-resource",
            signInSetup: "dcr",
          }}
          open
          onOpenChange={() => {}}
        />
      </MemoryRouter>,
    );

    // Filled in, with the rarely changed choices behind Details.
    expect((screen.getByLabelText("Name") as HTMLInputElement).value).toBe(
      "Tracker",
    );
    expect(
      screen.getByText("Each person signs in as themselves."),
    ).toBeTruthy();
    expect(screen.queryByText("issuer select")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Details" }));
    expect(screen.getByText("issuer select")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Make official" }));

    expect(await screen.findByText("Official server linked")).toBeTruthy();
    expect(mocks.create).toHaveBeenCalledWith(
      expect.objectContaining({
        url: "https://tracker.example.com/mcp",
        name: "Tracker",
        identityMode: "user",
        userSessionIssuerId: "issuer-1",
      }),
    );
    expect(mocks.save).toHaveBeenCalledWith({
      serverUrl: "https://tracker.example.com/mcp",
      mcpServerId: "server-1",
      allowDirectAccess: false,
    });
    const open = screen.getByRole("link", { name: "Open" });
    expect(open.getAttribute("href")).toBe(
      "/acme/projects/default/mcp/tracker",
    );
    expect(open.getAttribute("target")).toBe("_blank");
  });

  it("says when the new server still needs its sign-in set up", async () => {
    mocks.create.mockResolvedValue({
      mcpServer: { id: "server-1", slug: "tracker", name: "Tracker" },
      identityConfiguration: {
        status: "setup-required",
        message: "Automatic client registration is unavailable.",
      },
    });
    render(
      <MemoryRouter>
        <MakeOfficialSheet
          server={server()}
          probe={null}
          open
          onOpenChange={() => {}}
        />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Make official" }));
    expect(
      await screen.findByText(/stays off until its sign-in is set up/),
    ).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "Finish setup" }).getAttribute("target"),
    ).toBe("_blank");
    // An off server cannot be linked, so the sheet does not try, and says
    // how to link it instead of reporting a failed link.
    expect(mocks.save).not.toHaveBeenCalled();
    expect(screen.getByText("Server added")).toBeTruthy();
    expect(screen.getByText(/then link it from this page/)).toBeTruthy();
    expect(screen.queryByText(/The link could not be saved/)).toBeNull();
  });
});
