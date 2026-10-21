import { renderHook } from "@testing-library/react";
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
  search: "?range=30d&from=2025-01-01&to=2025-01-31&tab=ignored",
  navigate: vi.fn(),
  href: vi.fn((urn: string) => `/identities/${urn}/overview`),
  queryOptions: undefined as Record<string, unknown> | undefined,
  queryData: undefined as Array<{ id: string; name: string }> | undefined,
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
}));
vi.mock("@/routes", () => ({
  useRoutes: (overrides: unknown) => {
    expect(overrides).toEqual({ projectSlug: mocks.projectSlug });
    return {
      identities: { detail: { overview: { href: mocks.href } } },
    };
  },
}));
vi.mock("react-router", () => ({
  useLocation: () => ({ search: mocks.search }),
  useNavigate: () => mocks.navigate,
}));
vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: Record<string, unknown>) => {
    mocks.queryOptions = options;
    return { data: mocks.queryData };
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
    mocks.queryData = undefined;
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
      queryKey: ["identities", "registered-agents", "org-one", "user-one"],
      enabled: false,
      retry: false,
      throwOnError: false,
    });

    const signal = new AbortController().signal;
    await (
      mocks.queryOptions!.queryFn as (context: {
        signal: AbortSignal;
      }) => Promise<unknown>
    )({ signal });
    expect(mocks.fetchRegisteredAgents).toHaveBeenCalledWith(mocks.sdk, signal);
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
    mocks.queryData = [{ id: "agent-one", name: "Build bot" }];
    restrict();

    const { result } = renderHook(() =>
      useAgentIdentityCandidates({ enabled: true }),
    );

    expect(queryEnabled()).toBe(false);
    expect(result.current).toEqual([]);
  });

  it("maps names and canonical URNs without exposing them to intent ranking", () => {
    mocks.queryData = [{ id: "agent/one", name: "Release Agent" }];

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
});
