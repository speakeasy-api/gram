import type { ReactNode } from "react";
import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter, useNavigate } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { AccessMember, Role } from "@gram/client/models/components";
import { Operator } from "@gram/client/models/components";
import { DEFAULT_HOOK_TYPES } from "./observeFilterConstants";
import { useObserveFilters } from "./useObserveFilters";

vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: vi.fn(),
}));
vi.mock("@gram/client/react-query/roles.js", () => ({
  useRoles: vi.fn(),
}));

import { useMembers } from "@gram/client/react-query/members.js";
import { useRoles } from "@gram/client/react-query/roles.js";

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
      email: "alice@example.com",
      name: "Alice",
      roleId: "role-admin",
      joinedAt: new Date(),
      photoUrl: undefined,
    },
    {
      id: "m2",
      email: "bob@example.com",
      name: "Bob",
      roleId: "role-member",
      joinedAt: new Date(),
      photoUrl: undefined,
    },
  ];
  const mockRoles: Role[] = [
    {
      id: "role-admin",
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

    act(() => result.current.navigate(-1));

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

    act(() => result.current.navigate("/"));

    expect(result.current.filters.selectedHookTypes).toEqual(
      DEFAULT_HOOK_TYPES,
    );
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
});
