import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ShadowMCPPolicyUseCaseSection } from "./ShadowMCPPolicyUseCaseSection";
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

describe("ShadowMCPPolicyUseCaseSection", () => {
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  beforeEach(() => {
    mocks.useProject.mockReturnValue({ id: "project-1" });
  });

  it("renders risky servers for the policy journey", () => {
    mocks.useShadowMCPPolicyInventory.mockReturnValue({
      data: [
        {
          access: "none",
          accessSummary: testAccessSummary("none"),
          allowedPolicyIds: [],
          blockedPolicyIds: [],
          canonicalServerUrl: "https://shadow.example/mcp",
          firstSeen: new Date("2026-01-01T00:00:00Z"),
          lastCalled: new Date("2026-01-03T00:00:00Z"),
          lastSeen: new Date("2026-01-03T00:00:00Z"),
          observedUseCount: 4,
          requestCount: 2,
          serverName: "Risky Example",
          serverSlug: "risky-example",
          topUsers: [],
          urlHost: "shadow.example",
          userCount: 1,
        },
      ],
      isPending: false,
      isError: false,
      refetch: vi.fn(),
    });

    render(
      <MemoryRouter>
        <ShadowMCPPolicyUseCaseSection />
      </MemoryRouter>,
    );

    expect(screen.getByText("Top risky Shadow MCP servers")).toBeTruthy();
    expect(screen.getByText("Risky Example")).toBeTruthy();
    expect(screen.getByText("Open full inventory")).toBeTruthy();
  });
});
