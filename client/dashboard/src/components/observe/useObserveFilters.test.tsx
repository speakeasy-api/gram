import type { ReactNode } from "react";
import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter, useNavigate } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { Role } from "@gram/client/models/components/role.js";
import { Operator } from "@gram/client/models/components/logfilter.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import { DEFAULT_HOOK_TYPES } from "./observeFilterConstants";
import {
  scopedToolUsageDefaultTypes,
  TOOL_USAGE_DEFAULT_TYPES,
  TOOL_USAGE_VALID_TYPES,
} from "./observeTargetFilters";
import { useObserveFilters } from "./useObserveFilters";

vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: vi.fn(),
}));
vi.mock("@gram/client/react-query/roles.js", () => ({
  useRoles: vi.fn(),
}));
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
vi.mock("@gram/client/funcs/telemetryGetHooksSummary", () => ({
  telemetryGetHooksSummary: vi.fn().mockResolvedValue({
    ok: true,
    value: { servers: [], users: [] },
  }),
}));

function useObserveFiltersWithNavigation() {
  const filters = useObserveFilters();
  const navigate = useNavigate();
  return { filters, navigate };
}

describe("useObserveFilters", () => {
  afterEach(() => vi.clearAllMocks());

  const mockMembers: AccessMember[] = [
    {
      id: "m1",
      principalUrn: "user:m1",
      email: "alice@example.com",
      name: "Alice",
      roleIds: ["role-admin"],
      directoryRoleIds: [],
      joinedAt: new Date(),
      photoUrl: undefined,
    },
    {
      id: "m2",
      principalUrn: "user:m2",
      email: "bob@example.com",
      name: "Bob",
      roleIds: ["role-member"],
      directoryRoleIds: [],
      joinedAt: new Date(),
      photoUrl: undefined,
    },
  ];
  const mockRoles: Role[] = [
    {
      id: "role-admin",
      principalUrn: "role:organization:role-admin",
      name: "Admin",
      slug: "admin",
      description: "",
      isSystem: true,
      memberCount: 1,
      grants: [],
      createdAt: new Date(),
      updatedAt: new Date(),
    },
    {
      id: "role-member",
      principalUrn: "role:organization:role-member",
      name: "Member",
      slug: "member",
      description: "",
      isSystem: false,
      memberCount: 1,
      grants: [],
      createdAt: new Date(),
      updatedAt: new Date(),
    },
  ];

  function makeWrapper(initialUrl: string) {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const membersResult = {
      data: { members: mockMembers },
      isLoading: false,
    } satisfies Pick<ReturnType<typeof useMembers>, "data" | "isLoading">;
    const rolesResult = {
      data: { roles: mockRoles },
      isLoading: false,
    } satisfies Pick<ReturnType<typeof useRoles>, "data" | "isLoading">;
    vi.mocked(useMembers).mockReturnValue(
      membersResult as unknown as ReturnType<typeof useMembers>,
    );
    vi.mocked(useRoles).mockReturnValue(
      rolesResult as unknown as ReturnType<typeof useRoles>,
    );
    function Wrapper({ children }: { children: ReactNode }) {
      return (
        <QueryClientProvider client={qc}>
          <MemoryRouter initialEntries={[initialUrl]}>{children}</MemoryRouter>
        </QueryClientProvider>
      );
    }
    return Wrapper;
  }

  it("derives chips and hook types from browser navigation", () => {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const membersResult = {
      data: { members: [] },
      isLoading: false,
    } satisfies Pick<ReturnType<typeof useMembers>, "data" | "isLoading">;
    const rolesResult = {
      data: { roles: [] },
      isLoading: false,
    } satisfies Pick<ReturnType<typeof useRoles>, "data" | "isLoading">;
    vi.mocked(useMembers).mockReturnValue(
      membersResult as unknown as ReturnType<typeof useMembers>,
    );
    vi.mocked(useRoles).mockReturnValue(
      rolesResult as unknown as ReturnType<typeof useRoles>,
    );
    function RouterWrapper({ children }: { children: ReactNode }) {
      return (
        <QueryClientProvider client={qc}>
          <MemoryRouter
            initialEntries={[
              "/?server=api&user=alex@example.com&hookTypes=mcp",
              "/?server=api-v2&user=becca@example.com&hookTypes=local,skill",
            ]}
            initialIndex={1}
          >
            {children}
          </MemoryRouter>
        </QueryClientProvider>
      );
    }

    const { result } = renderHook(() => useObserveFiltersWithNavigation(), {
      wrapper: RouterWrapper,
    });

    expect(result.current.filters.activeFilters).toEqual([
      {
        display: "api-v2",
        filters: ["api-v2"],
        path: "gram.tool_call.source",
      },
      {
        display: "becca@example.com",
        filters: ["becca@example.com"],
        path: "user.email",
      },
    ]);
    expect(result.current.filters.selectedHookTypes).toEqual([
      "local",
      "skill",
    ]);

    void act(() => result.current.navigate(-1));

    expect(result.current.filters.activeFilters).toEqual([
      {
        display: "api",
        filters: ["api"],
        path: "gram.tool_call.source",
      },
      {
        display: "alex@example.com",
        filters: ["alex@example.com"],
        path: "user.email",
      },
    ]);
    expect(result.current.filters.selectedHookTypes).toEqual(["mcp"]);
  });

  it("falls back to default hook types when the URL param is cleared", () => {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const membersResult = {
      data: { members: [] },
      isLoading: false,
    } satisfies Pick<ReturnType<typeof useMembers>, "data" | "isLoading">;
    const rolesResult = {
      data: { roles: [] },
      isLoading: false,
    } satisfies Pick<ReturnType<typeof useRoles>, "data" | "isLoading">;
    vi.mocked(useMembers).mockReturnValue(
      membersResult as unknown as ReturnType<typeof useMembers>,
    );
    vi.mocked(useRoles).mockReturnValue(
      rolesResult as unknown as ReturnType<typeof useRoles>,
    );
    function RouterWrapper({ children }: { children: ReactNode }) {
      return (
        <QueryClientProvider client={qc}>
          <MemoryRouter initialEntries={["/?hookTypes=local"]}>
            {children}
          </MemoryRouter>
        </QueryClientProvider>
      );
    }

    const { result } = renderHook(() => useObserveFiltersWithNavigation(), {
      wrapper: RouterWrapper,
    });

    expect(result.current.filters.selectedHookTypes).toEqual(["local"]);

    void act(() => result.current.navigate("/"));

    expect(result.current.filters.selectedHookTypes).toEqual(
      DEFAULT_HOOK_TYPES,
    );
  });

  it("selects local tools by default when every type is the default", () => {
    // The identity page embeds Tool logs with every type as its default, so
    // an agent that only makes local tool calls through hooks still shows up.
    const { result } = renderHook(
      () =>
        useObserveFilters({
          defaultTypes: TOOL_USAGE_VALID_TYPES,
          validTypes: TOOL_USAGE_VALID_TYPES,
        }),
      { wrapper: makeWrapper("/") },
    );

    expect(result.current.selectedHookTypes).toEqual(TOOL_USAGE_VALID_TYPES);
    expect(result.current.selectedHookTypes).toContain("local_tool");
  });

  it("lets a Type selection in the URL win over the identity page's default", () => {
    // With local tools hidden on the identity page, a user who picks them in
    // the Type facet still sees them.
    const { result } = renderHook(
      () =>
        useObserveFilters({
          defaultTypes: scopedToolUsageDefaultTypes(false),
          validTypes: TOOL_USAGE_VALID_TYPES,
        }),
      { wrapper: makeWrapper("/?hookTypes=local_tool") },
    );

    expect(result.current.selectedHookTypes).toEqual(["local_tool"]);
  });

  it("leaves local tools out of the page-wide tool usage default", () => {
    const { result } = renderHook(
      () =>
        useObserveFilters({
          defaultTypes: TOOL_USAGE_DEFAULT_TYPES,
          validTypes: TOOL_USAGE_VALID_TYPES,
        }),
      { wrapper: makeWrapper("/") },
    );

    expect(result.current.selectedHookTypes).not.toContain("local_tool");
  });

  it("parses role URL param and exposes selectedRoleIds", () => {
    const { result } = renderHook(() => useObserveFilters(), {
      wrapper: makeWrapper("/?role=role-admin"),
    });
    expect(result.current.selectedRoleIds).toEqual(["role-admin"]);
  });

  it("exposes roleOptions derived from roles data", () => {
    const { result } = renderHook(() => useObserveFilters(), {
      wrapper: makeWrapper("/"),
    });
    expect(result.current.roleOptions).toEqual([
      { id: "role-admin", name: "Admin" },
      { id: "role-member", name: "Member" },
    ]);
  });

  it("includes resolved role emails in logFilters when role is selected", () => {
    const { result } = renderHook(() => useObserveFilters(), {
      wrapper: makeWrapper("/?role=role-admin"),
    });
    expect(result.current.logFilters).toEqual([
      {
        path: "user.email",
        operator: Operator.In,
        values: ["alice@example.com"],
      },
    ]);
  });

  it("handleRoleSelectionChange sets role URL param", () => {
    const { result } = renderHook(() => useObserveFiltersWithNavigation(), {
      wrapper: makeWrapper("/"),
    });
    act(() => result.current.filters.handleRoleSelectionChange(["role-admin"]));
    expect(result.current.filters.selectedRoleIds).toEqual(["role-admin"]);
  });

  it("handleRoleSelectionChange with empty array clears role URL param", () => {
    const { result } = renderHook(() => useObserveFiltersWithNavigation(), {
      wrapper: makeWrapper("/?role=role-admin"),
    });
    act(() => result.current.filters.handleRoleSelectionChange([]));
    expect(result.current.filters.selectedRoleIds).toEqual([]);
  });

  it("parses source URL param into a gram.hook.source chip and logFilters", () => {
    const { result } = renderHook(() => useObserveFilters(), {
      wrapper: makeWrapper("/?source=claude-code,codex"),
    });
    expect(result.current.activeFilters).toEqual([
      {
        display: "claude-code, codex",
        filters: ["claude-code", "codex"],
        path: "gram.hook.source",
      },
    ]);
    expect(result.current.logFilters).toEqual([
      {
        path: "gram.hook.source",
        operator: Operator.In,
        values: ["claude-code", "codex"],
      },
    ]);
  });

  it("handleHookSourceSelectionChange sets and clears the source URL param", () => {
    const { result } = renderHook(() => useObserveFiltersWithNavigation(), {
      wrapper: makeWrapper("/"),
    });
    act(() =>
      result.current.filters.handleHookSourceSelectionChange(["cursor"]),
    );
    expect(result.current.filters.activeFilters).toEqual([
      {
        display: "cursor",
        filters: ["cursor"],
        path: "gram.hook.source",
      },
    ]);
    act(() => result.current.filters.handleHookSourceSelectionChange([]));
    expect(result.current.filters.activeFilters).toEqual([]);
  });
});
