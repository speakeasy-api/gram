import { renderHook } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { isSendable } from "./types";

const state = vi.hoisted(() => ({
  org: "org-one",
  project: "project-one",
  allowed: true,
  navigate: vi.fn(),
  routeOptions: [] as Array<{ projectSlug: string }>,
  fetchNextPage: vi.fn<() => Promise<unknown>>(),
  next: false,
  nextError: false,
  queryCalls: [] as Array<Record<string, unknown>>,
  infiniteCalls: [] as Array<Record<string, unknown>>,
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: state.org }),
  useProject: () => ({ id: state.project }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({}),
  useProjectSlugForRequests: () => state.project,
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => state.allowed }),
}));
vi.mock("react-router", () => ({ useNavigate: () => state.navigate }));
vi.mock("@/routes", () => ({
  useRoutes: (options: { projectSlug: string }) => {
    state.routeOptions.push(options);
    return {
      mcp: {
        organizationGateway: { href: () => "/org/gateway" },
        gateway: {
          sessions: { href: (id: string) => `/gateway/${id}/sessions` },
        },
      },
    };
  },
}));
vi.mock("@gram/client/react-query/metaMcpServers.js", () => ({
  buildMetaMcpServersQuery: (_: unknown, input: unknown) => ({
    queryKey: ["gateways", input],
  }),
}));
vi.mock("@gram/client/react-query/userSessionClients.js", () => ({
  buildUserSessionClientsInfiniteQuery: (_: unknown, input: unknown) => ({
    queryKey: ["clients", input],
  }),
}));
vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: Record<string, unknown>) => {
    state.queryCalls.push(options);
    return {
      data: {
        metaMcpServers: [
          {
            id: "gateway-one",
            name: "Example gateway",
            userSessionIssuerId: "issuer-one",
          },
        ],
      },
      isError: false,
    };
  },
  useInfiniteQuery: (options: Record<string, unknown>) => {
    state.infiniteCalls.push(options);
    return {
      data: {
        pages: [
          {
            result: {
              items: [
                {
                  id: "unrelated",
                  clientName: "Other issuer",
                  userSessionIssuerId: "issuer-two",
                },
              ],
            },
          },
          {
            result: {
              items: [
                {
                  id: "later-page",
                  clientName: "Example desktop",
                  clientId: "public-client",
                  userSessionIssuerId: "issuer-one",
                },
              ],
            },
          },
        ],
      },
      hasNextPage: state.next,
      isFetchingNextPage: false,
      isFetchNextPageError: state.nextError,
      fetchNextPage: state.fetchNextPage,
    };
  },
}));
import { useGatewayConnectionCandidates } from "./useGatewayConnectionCandidates";
beforeEach(() => {
  state.org = "org-one";
  state.project = "project-one";
  state.allowed = true;
  state.next = false;
  state.nextError = false;
  state.navigate.mockReset();
  state.fetchNextPage.mockReset().mockResolvedValue(undefined);
  state.queryCalls.length = 0;
  state.infiniteCalls.length = 0;
});
it("finds a later-page registration and opens its canonical gateway with private matching", () => {
  const { result } = renderHook(() =>
    useGatewayConnectionCandidates({ enabled: true, inProject: true }),
  );
  expect(result.current.map((candidate) => candidate.title)).toEqual([
    "Organization Gateway Connections",
    "Example gateway",
    "Example desktop",
  ]);
  const registration = result.current[2]!;
  expect(registration.keywords).toContain("public-client");
  expect(isSendable(registration)).toBe(false);
  void registration.run("open");
  expect(state.navigate).toHaveBeenCalledWith(
    "/gateway/gateway-one/sessions?registration=later-page",
  );
  void result.current[0]!.run("open");
  expect(state.navigate).toHaveBeenCalledWith("/org/gateway?tab=connections");
});
it("does not fetch or return cached resources while closed or unauthorized", () => {
  state.next = true;
  const { result, rerender } = renderHook(
    ({ enabled }) =>
      useGatewayConnectionCandidates({ enabled, inProject: true }),
    { initialProps: { enabled: false } },
  );
  expect(result.current).toEqual([]);
  expect(state.queryCalls.at(-1)?.enabled).toBe(false);
  expect(state.infiniteCalls.at(-1)?.enabled).toBe(false);
  expect(state.fetchNextPage).not.toHaveBeenCalled();
  state.allowed = false;
  rerender({ enabled: true });
  expect(result.current).toEqual([]);
  expect(state.queryCalls.at(-1)?.enabled).toBe(false);
  expect(state.infiniteCalls.at(-1)?.enabled).toBe(false);
});
it("drains inventory only while open and stops after a page error", () => {
  state.next = true;
  const { rerender } = renderHook(() =>
    useGatewayConnectionCandidates({ enabled: true, inProject: true }),
  );
  expect(state.fetchNextPage).toHaveBeenCalledTimes(1);
  state.nextError = true;
  rerender();
  expect(state.fetchNextPage).toHaveBeenCalledTimes(1);
});
it("keys both inventories by organization and project and offers org navigation outside a project", () => {
  const { result, rerender } = renderHook(
    ({ inProject }) =>
      useGatewayConnectionCandidates({ enabled: true, inProject }),
    { initialProps: { inProject: true } },
  );
  expect(state.queryCalls.at(-1)?.queryKey).toEqual([
    "gateways",
    { gramProject: "project-one" },
    "org-one",
  ]);
  expect(state.infiniteCalls.at(-1)?.queryKey).toEqual([
    "clients",
    { gramProject: "project-one", limit: 100 },
    "org-one",
  ]);
  expect(state.routeOptions.at(-1)).toEqual({ projectSlug: "project-one" });
  state.org = "org-two";
  state.project = "project-two";
  rerender({ inProject: false });
  expect(result.current.map((c) => c.title)).toEqual([
    "Organization Gateway Connections",
  ]);
  expect(state.queryCalls.at(-1)?.enabled).toBe(false);
  expect(state.queryCalls.at(-1)?.queryKey).toEqual([
    "gateways",
    { gramProject: "project-two" },
    "org-two",
  ]);
});
