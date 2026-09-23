import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import type { IdentityModel } from "@gram/client/models/components/identitymodel.js";
import type { SlackPersonAccount } from "@gram/client/models/components/slackpersonaccount.js";
import { IdentityWorkIdentities } from "./IdentityWorkIdentities";

const mocks = vi.hoisted(() => ({
  query: vi.fn(),
  options: vi.fn(),
  admin: false,
  loadingAccess: false,
  featureState: "enabled",
  pending: false,
  error: false,
  refetch: vi.fn(),
  accounts: [] as SlackPersonAccount[],
  cursor: undefined as string | undefined,
}));
vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({
    user: { id: "user_self", email: "self@demo.getgram.ai" },
  }),
  useOrganization: () => ({ id: "org_example" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string, id: string) =>
      scope === "org:admin" && id === "org_example" && mocks.admin,
    isLoading: mocks.loadingAccess,
  }),
}));
vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  useProductFeatures: () => ({
    data:
      mocks.featureState === "missing"
        ? undefined
        : { claudeTagSupportEnabled: mocks.featureState !== "disabled" },
    isPending: mocks.featureState === "loading",
    isError: mocks.featureState === "error",
  }),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({ identity: { href: () => "/example/identity" } }),
}));
vi.mock("@/lib/dates", () => ({
  HumanizeDateTime: ({ date }: { date: Date }) => (
    <time>{date.toISOString()}</time>
  ),
}));
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
vi.mock("@gram/client/react-query/slackPersonAccounts.js", () => ({
  buildSlackPersonAccountsQuery: (_client: unknown, ...args: unknown[]) => {
    mocks.query(...args);
    return { queryKey: ["personal-slack-test"], queryFn: vi.fn() };
  },
}));
vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: unknown) => {
    mocks.options(options);
    return {
      data: mocks.pending
        ? undefined
        : { accounts: mocks.accounts, nextCursor: mocks.cursor },
      isPending: mocks.pending,
      isFetching: mocks.pending,
      isError: mocks.error,
      refetch: mocks.refetch,
    };
  },
}));
const identity: IdentityModel = {
  canonicalUrn: "user:user_self",
  kind: "user",
  displayName: "Synthetic Person",
  userIds: ["user_self"],
  emails: ["self@demo.getgram.ai"],
  externalUserIds: [],
  directory: { groups: [] },
};
const account: SlackPersonAccount = {
  member: {
    id: "00000000-0000-4000-8000-000000000001",
    connectionId: "00000000-0000-4000-8000-000000000002",
    workspaceId: "TEXAMPLE01",
    workspaceName: "Example Engineering",
    slackUserId: "UEXAMPLE01",
    displayName: "Synthetic Person",
    email: "self@demo.getgram.ai",
    status: "active",
    memberType: "person",
    lastSeenAt: new Date("2026-01-01"),
    observedInLastSync: true,
    mappingRevision: 1,
    observationToken: "synthetic-evidence",
    mappingStatus: "mapped",
    mapping: {
      id: "synthetic-mapping",
      userId: "user_self",
      displayName: "Synthetic Person",
      email: "self@demo.getgram.ai",
      active: true,
    },
  },
  directoryStatus: "current",
  lastFullSyncSucceededAt: new Date("2026-01-01"),
};
function show(subject = identity) {
  return render(
    <MemoryRouter>
      <IdentityWorkIdentities identity={subject} />
    </MemoryRouter>,
  );
}
beforeEach(() => {
  vi.clearAllMocks();
  mocks.admin = false;
  mocks.loadingAccess = false;
  mocks.featureState = "enabled";
  mocks.pending = false;
  mocks.error = false;
  mocks.accounts = [account];
  mocks.cursor = undefined;
});
afterEach(cleanup);
it("reads an exact self ID and offers only contact-admin guidance", () => {
  show();
  expect(mocks.query).toHaveBeenCalledWith(
    { userId: "user_self", cursor: undefined },
    { sessionHeaderGramSession: "" },
  );
  expect(mocks.options).toHaveBeenCalledWith(
    expect.objectContaining({ retry: false }),
  );
  expect(
    screen.getByText(/contact your organization administrator/),
  ).toBeTruthy();
  expect(screen.queryByRole("link", { name: /Review/ })).toBeNull();
  expect(
    screen.queryByRole("button", { name: /map|edit|connect|confirm/i }),
  ).toBeNull();
});
it("does not query an unrelated employee even when their emails include the caller", () => {
  show({
    ...identity,
    canonicalUrn: "user:user_other",
    userIds: ["user_other"],
  });
  expect(mocks.query).not.toHaveBeenCalled();
  expect(screen.queryByText("Work identities")).toBeNull();
});
it.each(["unattributed", "agent", "apikey"] as const)(
  "does not query a %s identity",
  (kind) => {
    mocks.admin = true;
    show({ ...identity, kind });
    expect(mocks.query).not.toHaveBeenCalled();
  },
);
it.each([{ userIds: [] }, { userIds: ["user_self", "user_other"] }])(
  "requires one stable ID, without email fallback (%j)",
  ({ userIds }) => {
    show({ ...identity, userIds });
    expect(mocks.query).not.toHaveBeenCalled();
  },
);
it("rejects a canonical identity that does not match the stable ID", () => {
  show({ ...identity, canonicalUrn: "user:user_other" });
  expect(mocks.query).not.toHaveBeenCalled();
});
it.each(["disabled", "missing", "loading", "error"])(
  "does not request mappings with product feature %s",
  (status) => {
    mocks.featureState = status;
    show();
    expect(mocks.query).not.toHaveBeenCalled();
  },
);
it("waits for admin authorization before requesting another person's accounts", () => {
  mocks.admin = true;
  mocks.loadingAccess = true;
  show({
    ...identity,
    canonicalUrn: "user:user_other",
    userIds: ["user_other"],
  });
  expect(mocks.query).not.toHaveBeenCalled();
});
it("admins see all memberships and deep-link to the exact organization membership", () => {
  mocks.admin = true;
  mocks.accounts = [
    account,
    {
      ...account,
      member: {
        ...account.member,
        id: "00000000-0000-4000-8000-000000000003",
        connectionId: "00000000-0000-4000-8000-000000000004",
        workspaceName: "Example Operations",
      },
    },
  ];
  show({
    ...identity,
    canonicalUrn: "user:user_other",
    userIds: ["user_other"],
  });
  expect(mocks.query).toHaveBeenCalledWith(
    expect.objectContaining({ userId: "user_other" }),
    expect.anything(),
  );
  const link = screen.getByRole("link", {
    name: /Review Slack mapping.*Example Operations/,
  });
  const url = new URL(link.getAttribute("href")!, "https://example.test");
  expect(url.pathname).toBe("/example/identity");
  expect(url.searchParams.get("slack_member")).toBe(
    "00000000-0000-4000-8000-000000000003",
  );
  expect(url.searchParams.get("slack_workspace")).toBe(
    "00000000-0000-4000-8000-000000000004",
  );
  expect(url.searchParams.get("slack_view")).toBe("members");
  expect(url.searchParams.get("tab")).toBe("slack-workspaces");
  expect(screen.getAllByText("UEXAMPLE01")).toHaveLength(2);
});
it("keeps source state, review finding and directory freshness distinct", () => {
  mocks.accounts = [
    {
      ...account,
      directoryStatus: "stale",
      member: {
        ...account.member,
        status: "deactivated",
        observedInLastSync: false,
        mappingStatus: "needs_review",
        mappingConflictReason: "member_deactivated",
      },
    },
  ];
  show();
  for (const label of [
    "Deactivated",
    "Needs review",
    "Slack account was deactivated",
    "Stale directory",
    "Not seen in last sync",
  ])
    expect(screen.getByText(label)).toBeTruthy();
  expect(screen.getByText(/Last full sync/)).toBeTruthy();
});
it("shows a no-mapping empty state only after a successful read", () => {
  mocks.accounts = [];
  show();
  expect(screen.getByText("No mapped Slack accounts.")).toBeTruthy();
});
it("does not flash an empty state while loading", () => {
  mocks.pending = true;
  show();
  expect(screen.queryByText("No mapped Slack accounts.")).toBeNull();
});
it("hides cached accounts on an access error and lets the caller retry", () => {
  const page = show();
  expect(screen.getByText("UEXAMPLE01")).toBeTruthy();
  mocks.error = true;
  page.rerender(
    <MemoryRouter>
      <IdentityWorkIdentities identity={identity} />
    </MemoryRouter>,
  );
  expect(screen.queryByText("UEXAMPLE01")).toBeNull();
  expect(screen.queryByText("No mapped Slack accounts.")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Try again" }));
  expect(mocks.refetch).toHaveBeenCalledOnce();
});
it("pages through memberships without changing the subject", () => {
  mocks.cursor = account.member.id;
  show();
  fireEvent.click(screen.getByRole("button", { name: "Next accounts" }));
  expect(mocks.query).toHaveBeenLastCalledWith(
    { userId: "user_self", cursor: account.member.id },
    expect.anything(),
  );
  fireEvent.click(screen.getByRole("button", { name: "Previous accounts" }));
  expect(mocks.query).toHaveBeenLastCalledWith(
    { userId: "user_self", cursor: undefined },
    expect.anything(),
  );
});
