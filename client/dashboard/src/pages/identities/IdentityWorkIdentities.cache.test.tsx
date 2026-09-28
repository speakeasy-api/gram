import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { invalidateAllSlackPersonAccounts } from "@gram/client/react-query/slackPersonAccounts.js";
import type { IdentityModel } from "@gram/client/models/components/identitymodel.js";
import type { SlackPersonAccount } from "@gram/client/models/components/slackpersonaccount.js";
import { IdentityWorkIdentities } from "./IdentityWorkIdentities";

const mocks = vi.hoisted(() => ({
  organizationId: "org_example_a",
  fetch: vi.fn(),
}));
vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({ user: { id: "user_self" } }),
  useOrganization: () => ({ id: mocks.organizationId }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => false, isLoading: false }),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({ identity: { href: () => "/example/identity" } }),
}));
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
vi.mock(
  "@gram/client/react-query/slackPersonAccounts.js",
  async (importOriginal) => {
    const actual =
      await importOriginal<
        typeof import("@gram/client/react-query/slackPersonAccounts.js")
      >();
    return {
      ...actual,
      buildSlackPersonAccountsQuery: (
        ...args: Parameters<typeof actual.buildSlackPersonAccountsQuery>
      ) => ({
        ...actual.buildSlackPersonAccountsQuery(...args),
        queryFn: mocks.fetch,
      }),
    };
  },
);

const identity: IdentityModel = {
  canonicalUrn: "user:user_self",
  kind: "user",
  displayName: "Synthetic Person",
  userIds: ["user_self"],
  emails: [],
  externalUserIds: [],
  directory: { groups: [] },
};
const account: SlackPersonAccount = {
  member: {
    id: "00000000-0000-4000-8000-000000000001",
    connectionId: "00000000-0000-4000-8000-000000000002",
    workspaceId: "TEXAMPLE01",
    workspaceName: "First organization workspace",
    slackUserId: "UEXAMPLE01",
    displayName: "First organization account",
    status: "active",
    memberType: "person",
    lastSeenAt: new Date("2026-01-01"),
    observedInLastSync: true,
    mappingRevision: 1,
    observationToken: "synthetic-evidence",
    mappingStatus: "mapped",
  },
  directoryStatus: "never_synced",
};
afterEach(cleanup);

it("isolates cached memberships by organization while preserving broad mapping invalidation", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const next = Promise.withResolvers<{ accounts: SlackPersonAccount[] }>();
  mocks.fetch
    .mockResolvedValueOnce({ accounts: [account] })
    .mockReturnValueOnce(next.promise);
  const panel = (
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <IdentityWorkIdentities identity={identity} />
      </MemoryRouter>
    </QueryClientProvider>
  );
  const page = render(panel);
  expect(await screen.findByText("First organization account")).toBeTruthy();
  mocks.organizationId = "org_example_b";
  // Rerender in one JS lifetime to exercise cache isolation independently of hard navigation.
  page.rerender(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <IdentityWorkIdentities identity={identity} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  expect(screen.queryByText("First organization account")).toBeNull();
  await waitFor(() => expect(mocks.fetch).toHaveBeenCalledTimes(2));
  next.resolve({ accounts: [] });
  expect(await screen.findByText("No mapped Slack accounts.")).toBeTruthy();
  await invalidateAllSlackPersonAccounts(client, { refetchType: "none" });
  const entries = client.getQueryCache().getAll();
  expect(entries).toHaveLength(2);
  expect(entries.map((entry) => entry.queryKey.at(-1))).toEqual([
    { organizationId: "org_example_a" },
    { organizationId: "org_example_b" },
  ]);
  expect(entries.every((entry) => entry.state.isInvalidated)).toBe(true);
  client.clear();
});
