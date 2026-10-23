import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({
  isAdmin: true,
  orgId: "org-one",
  projectSlug: "project-one",
  servers: [{ id: "builtin-one", key: "slack", name: "Slack", hidden: false }],
  isError: false,
  list: vi.fn(),
  hasScope: vi.fn(),
  goTo: vi.fn(),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: state.orgId }),
  useProject: () => ({ id: `${state.orgId}:${state.projectSlug}` }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => state.projectSlug,
  useSdkClient: () => ({}),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: state.hasScope }),
}));
vi.mock("@gram/client/react-query/builtinMcpServers.js", () => ({
  buildBuiltinMcpServersQuery: (_client: unknown, request: unknown) => ({
    queryKey: ["builtin", request],
  }),
}));
vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: unknown) => {
    state.list(options);
    return { data: { servers: state.servers }, isError: state.isError };
  },
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({ mcp: { builtin: { goTo: state.goTo } } }),
}));

import { useBuiltinMcpCandidates } from "./useBuiltinMcpCandidates";

function latestOptions() {
  return state.list.mock.calls.at(-1)?.[0] as {
    enabled: boolean;
    throwOnError: boolean;
  };
}

describe("built-in MCP candidates", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    state.isAdmin = true;
    state.orgId = "org-one";
    state.isError = false;
    state.projectSlug = "project-one";
    state.servers = [
      { id: "builtin-one", key: "slack", name: "Slack", hidden: false },
    ];
    state.hasScope.mockImplementation(
      (scope, id) =>
        scope === "org:admin" && id === state.orgId && state.isAdmin,
    );
  });
  it("searches the live visible catalog and opens the canonical detail without connection verbs", () => {
    const { result } = renderHook(() =>
      useBuiltinMcpCandidates({ enabled: true }),
    );
    expect(result.current).toHaveLength(1);
    expect(result.current[0]).toMatchObject({
      title: "Slack",
      verbs: ["open"],
      keywords: expect.arrayContaining(["slack", "builtin", "builtin-one"]),
    });
    result.current[0]!.run("open");
    expect(state.goTo).toHaveBeenCalledWith("slack");
    expect(state.hasScope).toHaveBeenCalledWith("org:admin", "org-one");
  });
  it("does not fetch while closed or for non-admins and never leaks cached rows to non-admins", () => {
    const { result, rerender } = renderHook(
      ({ enabled }) => useBuiltinMcpCandidates({ enabled }),
      { initialProps: { enabled: false } },
    );
    expect(latestOptions().enabled).toBe(false);
    state.isAdmin = false;
    rerender({ enabled: true });
    expect(latestOptions().enabled).toBe(false);
    expect(result.current).toEqual([]);
  });
  it("omits hidden servers and failed reads", () => {
    state.servers[0]!.hidden = true;
    const { result, rerender } = renderHook(() =>
      useBuiltinMcpCandidates({ enabled: true }),
    );
    expect(result.current).toEqual([]);
    state.servers[0]!.hidden = false;
    state.isError = true;
    rerender();
    expect(result.current).toEqual([]);
  });
  it("keys requests by the current project and fails without throwing", () => {
    const { rerender } = renderHook(() =>
      useBuiltinMcpCandidates({ enabled: true }),
    );
    expect(state.list.mock.calls.at(-1)?.[0]).toMatchObject({
      queryKey: [
        "builtin",
        { gramProject: "project-one" },
        "org-one",
        "org-one:project-one",
      ],
    });
    state.projectSlug = "project-two";
    rerender();
    expect(state.list.mock.calls.at(-1)?.[0]).toMatchObject({
      queryKey: [
        "builtin",
        { gramProject: "project-two" },
        "org-one",
        "org-one:project-two",
      ],
    });
    state.orgId = "org-two";
    rerender();
    expect(state.list.mock.calls.at(-1)?.[0]).toMatchObject({
      queryKey: [
        "builtin",
        { gramProject: "project-two" },
        "org-two",
        "org-two:project-two",
      ],
    });
    expect(latestOptions().throwOnError).toBe(false);
  });
});
