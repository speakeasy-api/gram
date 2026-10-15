import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { ReactNode } from "react";

const state = vi.hoisted(() => ({
  failed: false,
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
vi.mock("@/contexts/Sdk", () => ({ useSdkClient: () => ({}) }));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@/components/role-link", () => ({
  RoleLink: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("./ChangeRoleDialog", () => ({
  ChangeRoleDialog: () => <div>Role editor</div>,
}));
vi.mock("./RemoveAssignmentDialog", () => ({
  RemoveAssignmentDialog: () => {
    state.editor();
    return <div>Removal dialog</div>;
  },
}));
vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: () => ({
    data: { members: rows.map((row) => row.member) },
    dataUpdatedAt: 1,
    isSuccess: true,
    refetch: vi.fn(),
  }),
}));
vi.mock("@gram/client/react-query/roles.js", () => ({
  useRoles: () => ({ data: { roles: [] }, isSuccess: true, refetch: vi.fn() }),
}));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: () => ({
    data: rows,
    isSuccess: !state.failed,
    isError: state.failed,
    isFetching: false,
    refetch: vi.fn(),
  }),
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
  vi.clearAllMocks();
});

it("starts with most servers assigned and includes people with no assignments", () => {
  const ready = vi.fn<(ready: boolean) => void>();
  render(
    <MemoryRouter>
      <MCPAssignments onReady={ready} />
    </MemoryRouter>,
  );
  const people = screen
    .getAllByRole("row")
    .slice(1)
    .map((row) => row.textContent);
  expect(people[0]).toContain("Morgan");
  expect(people[1]).toContain("Lee");
  expect(people[2]).toContain("Sam");
  expect(ready).toHaveBeenLastCalledWith(true);
});

it("loads sources only for an expanded person and defers editing controls", () => {
  render(
    <MemoryRouter>
      <MCPAssignments />
    </MemoryRouter>,
  );
  expect(state.explain).not.toHaveBeenCalled();
  expect(state.editor).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("row", { name: /Morgan/ }));
  expect(state.explain).toHaveBeenCalledWith({
    resourceKind: "mcp",
    resourceId: "a",
    userId: "morgan",
  });
  expect(screen.getAllByText("Reader")).toHaveLength(2);
  expect(state.editor).not.toHaveBeenCalled();
  const serverLink = screen.getByRole("link", { name: "Open Server a" });
  expect(serverLink.getAttribute("href")).toBe(
    "/test-org/projects/default/mcp/x/a/overview",
  );
  expect(serverLink.textContent).toBe("Server a");
  expect(serverLink.querySelector("[data-slot=avatar]")).toBeNull();
  expect(screen.getAllByLabelText("Default project")).toHaveLength(2);
});

it("refuses a complete review when assignments failed even if cached rows exist", () => {
  state.failed = true;
  const ready = vi.fn<(ready: boolean) => void>();
  render(
    <MemoryRouter>
      <MCPAssignments onReady={ready} />
    </MemoryRouter>,
  );
  expect(screen.getByRole("alert").textContent).toContain(
    "complete assignment review",
  );
  expect(screen.queryByRole("table")).toBeNull();
  expect(ready).toHaveBeenLastCalledWith(false);
});

it("shows the role editor only on expanded rows", () => {
  render(
    <MemoryRouter>
      <MCPAssignments />
    </MemoryRouter>,
  );
  expect(screen.getByRole("columnheader", { name: "Roles" })).toBeDefined();
  expect(screen.queryByRole("columnheader", { name: "Department" })).toBeNull();
  expect(
    screen.queryByRole("button", { name: "Edit roles for Morgan" }),
  ).toBeNull();
  fireEvent.click(screen.getByRole("row", { name: /Morgan/ }));
  fireEvent.click(
    screen.getByRole("button", { name: "Edit roles for Morgan" }),
  );
  expect(screen.getByText("Role editor")).toBeDefined();
  fireEvent.click(screen.getByRole("row", { name: /Morgan/ }));
  expect(
    screen.queryByRole("button", { name: "Edit roles for Morgan" }),
  ).toBeNull();
});
