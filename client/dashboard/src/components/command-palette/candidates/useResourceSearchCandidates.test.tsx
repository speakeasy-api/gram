import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  COMMAND_SEARCH_SCHEMA_VERSION,
  MIGRATED_RESOURCE_KINDS,
  createGrantFingerprint,
  mapResourceSearchItems,
  useResourceSearchCandidates,
} from "./useResourceSearchCandidates";

const mocks = vi.hoisted(() => ({
  queryResult: {
    data: undefined as unknown,
    isError: false,
    fetchStatus: "idle",
  },
  queryOptions: undefined as undefined | Record<string, unknown>,
  searchResources: vi.fn(),
  orgId: "org-1",
  projectId: "project-1",
  projectSlug: "project-one",
  userId: "user-1",
  navigate: vi.fn(),
  goTo: vi.fn(),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: Record<string, unknown>) => {
    mocks.queryOptions = options;
    return mocks.queryResult;
  },
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: mocks.orgId }),
  useProject: () => ({ id: mocks.projectId }),
  useSession: () => ({ user: { id: mocks.userId } }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => mocks.projectSlug,
  useSdkClient: () => ({
    commandSearch: { searchResources: mocks.searchResources },
  }),
}));
vi.mock("react-router", () => ({ useNavigate: () => mocks.navigate }));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    assistants: { detail: { goTo: mocks.goTo } },
    environments: { environment: { goTo: mocks.goTo } },
    mcp: {
      gateway: { overview: { goTo: mocks.goTo } },
      details: { overview: { goTo: mocks.goTo } },
    },
    plugins: { detail: { goTo: mocks.goTo } },
    policyCenter: { href: () => "/guardrails" },
    skills: { detail: { overview: { goTo: mocks.goTo } } },
  }),
}));

afterEach(cleanup);
beforeEach(() => {
  mocks.queryResult = { data: undefined, isError: false, fetchStatus: "idle" };
  mocks.queryOptions = undefined;
  mocks.searchResources.mockReset();
  mocks.searchResources.mockResolvedValue({});
  mocks.orgId = "org-1";
  mocks.projectId = "project-1";
  mocks.projectSlug = "project-one";
  mocks.userId = "user-1";
});

function renderSearch(query = "skill", enabled = true, accessKey = "grants-a") {
  return renderHook(
    (props: { open: boolean; query: string; access: string }) =>
      useResourceSearchCandidates({
        enabled: props.open,
        query: props.query,
        accessKey: props.access,
      }),
    { initialProps: { open: enabled, query, access: accessKey } },
  );
}

function response(overrides: Record<string, unknown> = {}) {
  return {
    schemaVersion: COMMAND_SEARCH_SCHEMA_VERSION,
    generatedAt: "2026-01-01T00:00:00Z",
    items: [],
    partial: false,
    timedOut: false,
    failedKinds: [],
    ...overrides,
  };
}

describe("resource command search", () => {
  it("canonicalizes full effective grants independent of ordering", () => {
    const left = [
      { scope: "assistant:read", selectors: [{ resourceId: "assistant-b" }] },
      { scope: "mcp:read", selectors: [{ projectId: "project-1" }] },
    ];
    expect(createGrantFingerprint(left)).toBe(
      createGrantFingerprint([left[1], left[0]]),
    );
    expect(createGrantFingerprint(left)).not.toBe(
      createGrantFingerprint([
        { scope: "assistant:read", selectors: [{ resourceId: "assistant-a" }] },
        left[1],
      ]),
    );
  });

  it("keys by user, org, project, normalized query, schema, and grants", () => {
    const hook = renderSearch("  skill  ");
    expect(mocks.queryOptions?.queryKey).toEqual([
      "command-resource-search",
      "user-1",
      "org-1",
      "project-1",
      "project-one",
      "skill",
      COMMAND_SEARCH_SCHEMA_VERSION,
      "grants-a",
    ]);
    hook.rerender({ open: true, query: "server", access: "grants-b" });
    expect(mocks.queryOptions?.queryKey).toContain("server");
    expect(mocks.queryOptions?.queryKey).toContain("grants-b");
  });

  it("does not enable a request for a closed, organization, or short query", () => {
    renderSearch("s");
    expect(mocks.queryOptions?.enabled).toBe(false);
    renderSearch("skill", false);
    expect(mocks.queryOptions?.enabled).toBe(false);
  });

  it("passes the normalized query and AbortSignal to the SDK", async () => {
    renderSearch("  skill  ");
    const controller = new AbortController();
    const queryFn = mocks.queryOptions?.queryFn as (context: {
      signal: AbortSignal;
    }) => Promise<unknown>;
    await queryFn({ signal: controller.signal });
    expect(mocks.searchResources).toHaveBeenCalledWith(
      { gramProject: "project-one", q: "skill" },
      undefined,
      { signal: controller.signal },
    );
  });

  it("hides a stale response as soon as the query key changes", () => {
    mocks.queryResult = {
      data: response({
        items: [
          {
            id: "environment-1",
            kind: "environment",
            name: "Old environment",
            routeSelector: "old-environment",
          },
        ],
      }),
      isError: false,
      fetchStatus: "idle",
    };
    const hook = renderSearch("old");
    expect(hook.result.current.candidates).toHaveLength(1);
    mocks.queryResult = {
      data: undefined,
      isError: false,
      fetchStatus: "fetching",
    };
    act(() => hook.rerender({ open: true, query: "new", access: "grants-a" }));
    expect(hook.result.current.candidates).toEqual([]);
    expect(mocks.queryOptions?.queryKey).toContain("new");
  });

  it("falls back only failed kinds while preserving successful remote rows", () => {
    mocks.queryResult = {
      data: response({
        partial: true,
        timedOut: true,
        failedKinds: ["plugin"],
        items: [
          {
            id: "environment-1",
            kind: "environment",
            name: "Remote environment",
            routeSelector: "remote-environment",
          },
        ],
      }),
      isError: false,
      fetchStatus: "idle",
    };
    const result = renderSearch().result.current;
    expect(result.candidates.map((candidate) => candidate.title)).toEqual([
      "Remote environment",
    ]);
    expect([...result.fallbackKinds]).toEqual(["plugin"]);
    expect(result.metrics).toMatchObject({ complete: false, truncated: true });
  });

  it("falls back all migrated kinds on a whole-request error", () => {
    mocks.queryResult = { data: undefined, isError: true, fetchStatus: "idle" };
    const result = renderSearch().result.current;
    expect([...result.fallbackKinds]).toEqual(MIGRATED_RESOURCE_KINDS);
    expect(result.candidates).toEqual([]);
  });

  it("maps only exact-authorized kinds and exact plugin UUID routes", () => {
    const candidates = mapResourceSearchItems(
      [
        {
          id: "skill-1",
          kind: "skill",
          name: "Skill",
          routeSelector: "skill-slug",
          keywords: ["excluded-skill-alias"],
        },
        {
          id: "assistant-1",
          kind: "assistant",
          name: "Assistant",
          routeSelector: "assistant-1",
        },
        {
          id: "mcp-1",
          kind: "mcp_server",
          name: "MCP server",
          routeSelector: "mcp-1",
        },
        {
          id: "11111111-1111-4111-8111-111111111111",
          kind: "plugin",
          name: "Plugin",
          routeSelector: "11111111-1111-4111-8111-111111111111",
          keywords: ["plugin-alias"],
        },
      ],
      {
        assistants: { detail: { goTo: mocks.goTo } },
        environments: { environment: { goTo: mocks.goTo } },
        mcp: {
          gateway: { overview: { goTo: mocks.goTo } },
          details: { overview: { goTo: mocks.goTo } },
        },
        plugins: { detail: { goTo: mocks.goTo } },
        policyCenter: { href: () => "/guardrails" },
        skills: { detail: { overview: { goTo: mocks.goTo } } },
      } as never,
      mocks.navigate,
    );
    expect(candidates).toHaveLength(1);
    expect(candidates[0]?.keywords).toContain("plugin-alias");
    void candidates[0]?.run("open");
    expect(mocks.goTo).toHaveBeenCalledWith(
      "11111111-1111-4111-8111-111111111111",
    );
  });
});
