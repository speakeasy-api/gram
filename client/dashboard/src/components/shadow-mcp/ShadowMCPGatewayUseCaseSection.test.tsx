import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ShadowMCPGatewayUseCaseSection } from "./ShadowMCPGatewayUseCaseSection";
import { testAccessSummary } from "./shadowMCPInventoryTestFixtures";

const mocks = vi.hoisted(() => ({
  useProject: vi.fn(),
  useShadowMCPPolicyInventory: vi.fn(),
}));

vi.mock("@/contexts/Auth", () => ({
  useProject: mocks.useProject,
}));

vi.mock("./useShadowMCPPolicyInventory", () => ({
  useShadowMCPPolicyInventory: mocks.useShadowMCPPolicyInventory,
}));

vi.mock("@/components/ui/Skeleton", () => ({
  SkeletonTable: () => <div>Loading table</div>,
}));

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    shadowMCP: {
      href: () => "/shadow-mcp",
      detail: { goTo: vi.fn() },
    },
  }),
}));

describe("ShadowMCPGatewayUseCaseSection", () => {
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  beforeEach(() => {
    mocks.useProject.mockReturnValue({ id: "project-1" });
  });

  it("renders concentrated usage candidates for the gateway journey", () => {
    mocks.useShadowMCPPolicyInventory.mockReturnValue({
      data: [
        {
          access: "none",
          accessSummary: testAccessSummary("none"),
          allowedPolicyIds: [],
          blockedPolicyIds: [],
          canonicalServerUrl: "https://okta.example/mcp",
          firstSeen: new Date("2026-01-01T00:00:00Z"),
          lastCalled: new Date("2026-01-03T00:00:00Z"),
          lastSeen: new Date("2026-01-03T00:00:00Z"),
          observedUseCount: 40,
          requestCount: 0,
          serverName: "Okta Example",
          serverSlug: "okta-example",
          topUsers: [],
          urlHost: "okta.example",
          userCount: 2,
        },
      ],
      isPending: false,
      isError: false,
      refetch: vi.fn(),
    });

    render(
      <MemoryRouter>
        <ShadowMCPGatewayUseCaseSection />
      </MemoryRouter>,
    );

    expect(
      screen.getByText("Shadow MCP distribution opportunities"),
    ).toBeTruthy();
    expect(screen.getByText("Okta Example")).toBeTruthy();
    expect(screen.getByText("Open full inventory")).toBeTruthy();
  });
});
