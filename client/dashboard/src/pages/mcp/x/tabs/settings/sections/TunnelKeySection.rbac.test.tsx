import { TooltipProvider } from "@/components/ui/Tooltip";
import type { TunneledMcpServer } from "@gram/client/models/components/tunneledmcpserver.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TunnelKeySection } from "./TunnelKeySection";

// These tests run the real useRBAC matcher against loaded grants, so they
// catch a gate that omits the project dimension the server checks.

const PROJECT = "project-1";

type TestGrant = {
  scope: string;
  selectors?: Array<Record<string, string>>;
};

const state = vi.hoisted(() => ({
  grants: [] as TestGrant[],
  rotate: vi.fn(),
}));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-1" }),
  useProject: () => ({ id: PROJECT, slug: "default" }),
  useSession: () => ({ session: "session-1" }),
  useIsPlatformAdmin: () => false,
}));

vi.mock("@gram/client/react-query/grants.js", () => ({
  useGrants: () => ({ data: { grants: state.grants }, isLoading: false }),
}));

vi.mock("@/pages/sources/tunneled-mcp/hooks", () => ({
  useRotateTunneledMcpServerKey: () => ({
    mutateAsync: state.rotate,
    reset: vi.fn(() => {}),
    isPending: false,
  }),
}));

const tunnel = {
  id: "tunnel-1",
  projectId: PROJECT,
  name: "jamf",
  keyPrefix: "gram_tun_",
  environmentLinked: false,
  environmentLinkAuthorized: true,
} as unknown as TunneledMcpServer;

function section() {
  return (
    <QueryClientProvider client={new QueryClient()}>
      <TooltipProvider>
        <TunnelKeySection tunneledMcpServer={tunnel} />
      </TooltipProvider>
    </QueryClientProvider>
  );
}

function confirmButton(): HTMLButtonElement {
  return screen.getByRole("button", { name: /^rotate$/i }) as HTMLButtonElement;
}

beforeEach(() => {
  state.grants = [];
  state.rotate.mockReset();
});

afterEach(cleanup);

describe("TunnelKeySection authorization", () => {
  it("disables an open confirmation when a project write exclusion arrives", () => {
    state.grants = [{ scope: "mcp:write" }];
    const view = render(section());
    fireEvent.click(screen.getByRole("button", { name: /rotate key/i }));
    expect(confirmButton().disabled).toBe(false);

    state.grants = [
      { scope: "mcp:write" },
      {
        scope: "mcp:blocked_write",
        selectors: [
          { resourceKind: "mcp", resourceId: "*", projectId: PROJECT },
        ],
      },
    ];
    view.rerender(section());

    expect(confirmButton().disabled).toBe(true);
    expect(
      screen.getByText("Rotating the key needs mcp:write on this project."),
    ).toBeTruthy();
    fireEvent.click(confirmButton());
    expect(state.rotate).not.toHaveBeenCalled();
  });

  it("does not offer rotation on a write grant for another project", () => {
    state.grants = [
      {
        scope: "mcp:write",
        selectors: [
          { resourceKind: "mcp", resourceId: "*", projectId: "project-2" },
        ],
      },
    ];
    render(section());
    fireEvent.click(screen.getByRole("button", { name: /rotate key/i }));
    expect(screen.queryByText("Rotate Tunnel Key")).toBeNull();
  });
});
