import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  organization: { id: "org-one", slug: "acme" },
  session: {
    user: { id: "user-one" },
    organizationOverride: false,
    impersonatorEmail: undefined as string | undefined,
  },
  isPlatformAdmin: false,
  scopeOverride: null as string | null,
  sdk: { name: "sdk" },
  projectSlug: "project-one",
  routeProjectSlug: "project-one" as string | undefined,
  search: "?range=30d&from=2025-01-01&to=2025-01-31&tab=ignored",
  navigate: vi.fn(),
  href: vi.fn((urn: string) => `/identities/${urn}/overview`),
  queryOptions: undefined as Record<string, unknown> | undefined,
  queryData: undefined as
    | {
        pages: Array<{
          items: Array<{
            id: string;
            name: string;
            permissions?: { write: boolean };
          }>;
        }>;
      }
    | undefined,
  hasNextPage: false,
  isFetchingNextPage: false,
  isFetchNextPageError: false,
  fetchNextPage: vi.fn<() => Promise<unknown>>(),
  fetchRegisteredAgents: vi.fn(),
}));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => mocks.organization,
  useSession: () => mocks.session,
  useIsPlatformAdmin: () => mocks.isPlatformAdmin,
}));
vi.mock("@/components/dev-toolbar-utils", () => ({
  getRBACScopeOverrideHeader: () => mocks.scopeOverride,
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => mocks.sdk,
  useProjectSlugForRequests: () => mocks.projectSlug,
  useSlugs: () => ({ projectSlug: mocks.routeProjectSlug }),
}));
vi.mock("@/routes", () => ({
  useRoutes: (overrides: unknown) => {
    expect(overrides).toEqual({ projectSlug: mocks.projectSlug });
    return {
      identities: {
        detail: {
          overview: { href: mocks.href },
          permissions: {
            href: (urn: string) => `/identities/${urn}/permissions`,
          },
        },
      },
    };
  },
}));
vi.mock("react-router", () => ({
  useLocation: () => ({ search: mocks.search }),
  useNavigate: () => mocks.navigate,
}));
vi.mock("@tanstack/react-query", () => ({
  useInfiniteQuery: (options: Record<string, unknown>) => {
    mocks.queryOptions = options;
    return {
      data: mocks.queryData,
      hasNextPage: mocks.hasNextPage,
      isFetchingNextPage: mocks.isFetchingNextPage,
      isFetchNextPageError: mocks.isFetchNextPageError,
      fetchNextPage: mocks.fetchNextPage,
    };
  },
}));
vi.mock("@/pages/identities/identityRoster", () => ({
  fetchRegisteredAgents: mocks.fetchRegisteredAgents,
}));

import { useAgentIdentityCandidates } from "./useAgentIdentityCandidates";

function queryEnabled(): boolean {
  return mocks.queryOptions?.enabled as boolean;
}

describe("useAgentIdentityCandidates", () => {
  beforeEach(() => {
    mocks.organization.id = "org-one";
    mocks.organization.slug = "acme";
    mocks.session.user.id = "user-one";
    mocks.session.organizationOverride = false;
    mocks.session.impersonatorEmail = undefined;
    mocks.isPlatformAdmin = false;
    mocks.scopeOverride = null;
    mocks.projectSlug = "project-one";
    mocks.routeProjectSlug = "project-one";
    mocks.queryData = undefined;
    mocks.hasNextPage = false;
    mocks.isFetchingNextPage = false;
    mocks.isFetchNextPageError = false;
    mocks.fetchNextPage.mockReset().mockResolvedValue(undefined);
    mocks.queryOptions = undefined;
    mocks.navigate.mockReset();
    mocks.href.mockClear();
    mocks.fetchRegisteredAgents.mockReset();
  });

  it("shares the identity roster cache and stays disabled while closed", async () => {
    const { result } = renderHook(() =>
      useAgentIdentityCandidates({ enabled: false }),
    );

    expect(result.current).toEqual([]);
    expect(mocks.queryOptions).toMatchObject({
      queryKey: [
        "identities",
        "registered-agents",
        "org-one",
        "user-one",
        "",
        "asc",
        "[]",
        "[]",
        "",
      ],
      enabled: false,
      retry: false,
      throwOnError: false,
    });

    const signal = new AbortController().signal;
    await (
      mocks.queryOptions!.queryFn as (context: {
        signal: AbortSignal;
        pageParam: string | undefined;
      }) => Promise<unknown>
    )({ signal, pageParam: "next-cursor" });
    expect(mocks.fetchRegisteredAgents).toHaveBeenCalledWith(
      mocks.sdk,
      { cursor: "next-cursor", limit: 50, search: "", sortOrder: "asc" },
      signal,
    );
    const getNextPage = mocks.queryOptions!.getNextPageParam as (page: {
      nextCursor?: string;
    }) => string | undefined;
    expect(getNextPage({ nextCursor: "next" })).toBe("next");
    expect(getNextPage({})).toBeUndefined();
    expect(mocks.fetchNextPage).not.toHaveBeenCalled();
  });

  it.each([
    ["demo organization", () => (mocks.organization.slug = "acme-demo")],
    ["support session", () => (mocks.session.organizationOverride = true)],
    [
      "impersonated session",
      () => (mocks.session.impersonatorEmail = "operator@example.test"),
    ],
    ["RBAC override", () => (mocks.scopeOverride = "project:read")],
  ])("does not expose agents in a %s", (_label, restrict) => {
    mocks.queryData = {
      pages: [{ items: [{ id: "agent-one", name: "Build bot" }] }],
    };
    restrict();

    const { result } = renderHook(() =>
      useAgentIdentityCandidates({ enabled: true }),
    );

    expect(queryEnabled()).toBe(false);
    expect(result.current).toEqual([]);
  });

  it("maps names and canonical URNs without exposing them to intent ranking", () => {
    mocks.queryData = {
      pages: [{ items: [{ id: "agent/one", name: "Release Agent" }] }],
    };

    const { result } = renderHook(() =>
      useAgentIdentityCandidates({ enabled: true }),
    );

    expect(queryEnabled()).toBe(true);
    expect(result.current[0]).toMatchObject({
      id: "agent-identity:agent/one",
      kind: "person",
      title: "Release Agent",
      detail: "Agent identity",
      keywords: ["agent", "agent/one", "agent:agent/one"],
      fuzzyOnly: true,
      icon: "bot",
      group: "Identities",
      verbs: ["open"],
    });

    void result.current[0]!.run("open");
    expect(mocks.href).toHaveBeenCalledWith("agent%3Aagent%2Fone");
    expect(mocks.navigate).toHaveBeenCalledWith(
      "/identities/agent%3Aagent%2Fone/overview?range=30d&from=2025-01-01&to=2025-01-31",
    );
  });
  it("includes later pages and stops fetching when closed or pagination fails", async () => {
    mocks.queryData = {
      pages: [
        { items: [{ id: "first", name: "Build Agent" }] },
        { items: [{ id: "last", name: "Release Agent" }] },
      ],
    };
    mocks.hasNextPage = true;
    mocks.isFetchingNextPage = false;
    mocks.isFetchNextPageError = false;
    mocks.fetchNextPage.mockClear();
    const { result, rerender } = renderHook(
      ({ enabled }) => useAgentIdentityCandidates({ enabled }),
      { initialProps: { enabled: true } },
    );
    expect(result.current.map((candidate) => candidate.title)).toEqual([
      "Build Agent",
      "Release Agent",
    ]);
    expect(mocks.fetchNextPage).toHaveBeenCalledTimes(1);
    await act(async () => {
      await Promise.resolve();
    });
    mocks.isFetchNextPageError = true;
    rerender({ enabled: true });
    expect(mocks.fetchNextPage).toHaveBeenCalledTimes(1);
    mocks.isFetchNextPageError = false;
    rerender({ enabled: false });
    expect(result.current).toEqual([]);
    expect(mocks.fetchNextPage).toHaveBeenCalledTimes(1);
  });

  it("does not offer the project-scoped alias in the org shell despite the request default", () => {
    mocks.projectSlug = "default";
    mocks.routeProjectSlug = undefined;
    mocks.queryData = {
      pages: [
        {
          items: [
            { id: "editable", name: "Worker", permissions: { write: true } },
          ],
        },
      ],
    };
    const { result, rerender } = renderHook(() =>
      useAgentIdentityCandidates({ enabled: true }),
    );
    expect(result.current.map((candidate) => candidate.id)).toEqual([
      "agent-identity:editable",
    ]);
    mocks.routeProjectSlug = "project-one";
    mocks.projectSlug = "project-one";
    rerender();
    expect(result.current.map((candidate) => candidate.id)).toContain(
      "agent-inference-access:editable",
    );
  });

  it("adds an inference-access alias only for editable agents and keeps names private", () => {
    mocks.queryData = {
      pages: [
        {
          items: [
            { id: "read_only", name: "Viewer", permissions: { write: false } },
          ],
        },
        {
          items: [
            { id: "editable", name: "Worker", permissions: { write: true } },
          ],
        },
      ],
    };
    const { result } = renderHook(() =>
      useAgentIdentityCandidates({ enabled: true }),
    );
    const aliases = result.current.filter((candidate) =>
      candidate.id.startsWith("agent-inference-access:"),
    );
    expect(aliases).toHaveLength(1);
    expect(aliases[0]).toMatchObject({
      title: "Worker · Inference access",
      fuzzyOnly: true,
      keywords: expect.arrayContaining(["inference", "permissions"]),
    });
    void aliases[0]!.run("open");
    expect(mocks.navigate).toHaveBeenCalledWith(
      "/identities/agent%3Aeditable/permissions?range=30d&from=2025-01-01&to=2025-01-31",
    );
  });
});
