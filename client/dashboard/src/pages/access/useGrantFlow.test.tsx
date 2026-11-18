import { TooltipProvider } from "@/components/ui/Tooltip";
import type { ChallengeBucket } from "@gram/client/models/components/challengebucket.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({
  scopes: new Set<string>(),
  drawerOpen: vi.fn(),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => {
    const has = (scope: string) => state.scopes.has(scope);
    return {
      isLoading: false,
      hasScope: has,
      hasAnyScope: (scopes: string[]) => scopes.some(has),
      hasAllScopes: (scopes: string[]) => scopes.every(has),
    };
  },
}));
vi.mock("@/hooks/useScopeCatalogue", () => ({
  useScopeCatalogue: () => ({
    catalogue: new Map(),
    tasks: [],
    isLoading: false,
    isError: false,
    refetch: () => {},
  }),
}));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));
vi.mock("@gram/client/react-query/resolveChallenge.js", () => ({
  useResolveChallengeMutation: () => ({ mutate: vi.fn(), isPending: false }),
}));
vi.mock("@gram/client/react-query/challenges.js", () => ({
  invalidateAllChallenges: vi.fn(),
}));
vi.mock("@gram/client/react-query/challengeBuckets.js", () => ({
  invalidateAllChallengeBuckets: vi.fn(),
}));
vi.mock("./GrantDrawer", () => ({
  GrantDrawer: ({ open }: { open: boolean }) => {
    if (open) state.drawerOpen();
    return null;
  },
}));
vi.mock("./CreateRoleDialog", () => ({
  CreateRoleDialog: () => null,
}));

import { useGrantFlow } from "./useGrantFlow";

function deniedChallenge(): ChallengeBucket {
  return {
    id: "bucket-1",
    organizationId: "org-1",
    principalUrn: "user:other-user",
    principalType: "user",
    userEmail: "other@example.test",
    scope: "chat:read",
    outcome: "deny",
    reason: "no_grants",
    evaluatedGrantCount: 0,
    matchedGrantCount: 0,
    challengeCount: 1,
    challengeIds: ["challenge-1"],
    resourceKind: "chat",
    resourceId: "chat-1",
    firstSeen: new Date(),
    lastSeen: new Date(),
  } as ChallengeBucket;
}

function GrantCell() {
  const { actionsColumn, grantFlowPortals } = useGrantFlow();
  return (
    <>
      {actionsColumn.render?.(deniedChallenge())}
      {grantFlowPortals}
    </>
  );
}

function renderGrantCell() {
  render(
    <TooltipProvider>
      <GrantCell />
    </TooltipProvider>,
  );
}

afterEach(cleanup);
beforeEach(() => {
  state.scopes = new Set();
  state.drawerOpen.mockReset();
});

describe("useGrantFlow Grant button", () => {
  it("opens the grant drawer for organization admins", () => {
    state.scopes = new Set(["org:read", "org:admin"]);
    renderGrantCell();

    fireEvent.click(screen.getByRole("button", { name: "Grant" }));

    expect(state.drawerOpen).toHaveBeenCalled();
  });

  it("does not open the grant drawer for members without org:admin", () => {
    // A member can list challenges (org:read) and may even hold the
    // challenged scope, but cannot resolve them on the server.
    state.scopes = new Set(["org:read", "chat:read"]);
    renderGrantCell();

    fireEvent.click(screen.getByRole("button", { name: "Grant" }));

    expect(state.drawerOpen).not.toHaveBeenCalled();
  });
});
