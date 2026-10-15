import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { useEffect, type ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const state = vi.hoisted(() => ({
  failed: false,
  connectAllowed: true,
  updatedAt: 1,
  unmountEditor: vi.fn(),
  explain: vi.fn(),
  editor: vi.fn(),
}));
const server = (id: string) => ({
  id,
  name: `Server ${id}`,
  projectId: "project",
  projectSlug: "default",
  slug: id,
});
const member = (id: string, name: string) => ({
  id,
  name,
  email: `${id}@example.test`,
  roleIds: [],
  directoryRoleIds: [],
  principalUrn: `user:${id}`,
  joinedAt: new Date(),
});
const rows = [
  { member: member("lee", "Lee"), servers: [server("a")] },
  { member: member("morgan", "Morgan"), servers: [server("a"), server("b")] },
  { member: member("sam", "Sam"), servers: [] },
];
vi.mock("@/routes", () => ({
  useRoutes: ({ projectSlug }: { projectSlug: string }) => ({
    mcp: {
      x: {
        overview: {
          href: (slug: string) =>
            `/test-org/projects/${projectSlug}/mcp/x/${slug}/overview`,
        },
      },
    },
  }),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "test-org", scimEnabled: false }),
}));
vi.mock("@gram/client/react-query/getMcpMetadata.js", () => ({
  useGetMcpMetadata: () => ({ data: undefined }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    access: {
      explainResourceAccess: async () => ({
        levels: [
          { level: "use", allowed: state.connectAllowed },
          { level: "view", allowed: true },
        ],
      }),
      listIdentityAccess: async ({ userId }: { userId: string }) => ({
        servers: rows.find((row) => row.member.id === userId)?.servers ?? [],
      }),
    },
  }),
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@/components/role-link", () => ({
  RoleLink: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("./ChangeRoleDialog", () => ({ ChangeRoleDialog: () => null }));
vi.mock("./RemoveAssignmentDialog", () => ({
  RemoveAssignmentDialog: () => {
    state.editor();
    useEffect(
      () => () => {
        state.unmountEditor();
      },
      [],
    );
    return <div>Removal dialog</div>;
  },
}));
vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: () => ({
    data: { members: rows.map((row) => row.member) },
    dataUpdatedAt: state.updatedAt,
    isSuccess: true,
    refetch: vi.fn(),
  }),
}));
vi.mock("@gram/client/react-query/roles.js", () => ({
  useRoles: () => ({ data: { roles: [] }, isSuccess: true, refetch: vi.fn() }),
}));
vi.mock("@gram/client/react-query/explainResourceAccess.js", () => ({
  useExplainResourceAccess: (request: unknown) => {
    state.explain(request);
    return {
      data: {
        levels: [
          {
            level: "use",
            rules: [
              {
                principalUrn: "role:global:reader",
                displayName: "Reader",
                kind: "role",
                effect: "allows",
              },
            ],
          },
        ],
      },
    };
  },
}));
import { MCPAssignments } from "./MCPAssignments";
afterEach(cleanup);
beforeEach(() => {
  state.failed = false;
  state.connectAllowed = true;
  vi.clearAllMocks();
});

it("keeps the removal dialog mounted when its member query refreshes", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const app = (
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <MCPAssignments />
      </MemoryRouter>
    </QueryClientProvider>
  );
  const view = render(app);
  fireEvent.click(await screen.findByRole("row", { name: /Morgan/ }));
  fireEvent.click(
    screen.getByRole("button", {
      name: "Remove access to Server a for Morgan",
    }),
  );
  expect(screen.getByText("Removal dialog")).toBeDefined();
  state.updatedAt += 1;
  view.rerender(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <MCPAssignments />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  await waitFor(() => expect(screen.getByText("Removal dialog")).toBeDefined());
  expect(state.unmountEditor).not.toHaveBeenCalled();
});

it("excludes view-only servers when connect is blocked", async () => {
  state.connectAllowed = false;
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <MCPAssignments />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  fireEvent.click(await screen.findByRole("row", { name: /Morgan/ }));
  expect(screen.queryByRole("link", { name: "Open Server a" })).toBeNull();
  expect(
    screen.queryByRole("button", { name: /Remove access to Server a/ }),
  ).toBeNull();
});
