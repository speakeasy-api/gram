import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const queryHooks = vi.hoisted(() => ({
  overview: vi.fn(),
  policies: vi.fn(),
  servers: vi.fn(),
}));

const EMPTY_OVERVIEW = vi.hoisted(() => ({ empty: true }));

vi.mock("@/components/project-guide/allTimeOverviewQuery", () => ({
  isOverviewEmpty: (overview: unknown) => overview === EMPTY_OVERVIEW,
  useAllTimeProjectOverview: queryHooks.overview,
}));
vi.mock("@/components/project-guide/projectGuideStores", () => ({
  useProjectGuideDismissed: () => false,
  useProjectGuideStarted: () => false,
}));
vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "request-project",
  useSlugs: () => ({ orgSlug: "org", projectSlug: "route-project" }),
}));
vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => ({ status: "enabled" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => true, isLoading: false }),
}));
vi.mock("@gram/client/react-query/mcpServers.js", () => ({
  useMcpServers: queryHooks.servers,
}));
vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  useProductFeatures: () => ({
    data: { logsEnabled: true },
    isPending: false,
  }),
}));
vi.mock("@gram/client/react-query/riskListPolicies.js", () => ({
  useRiskListPolicies: queryHooks.policies,
}));

import { useProjectGuide } from "./useProjectGuide";

beforeEach(() => {
  vi.clearAllMocks();
  queryHooks.servers.mockReturnValue({
    data: { mcpServers: [] },
    isError: false,
    isPending: false,
  });
  queryHooks.policies.mockReturnValue({
    data: { policies: [] },
    isError: false,
    isPending: false,
  });
  queryHooks.overview.mockReturnValue({
    data: EMPTY_OVERVIEW,
    isError: false,
    isPending: false,
  });
});

describe("useProjectGuide", () => {
  it("scopes both cheap generated queries to the request project", () => {
    renderHook(() => useProjectGuide());

    expect(queryHooks.servers).toHaveBeenCalledWith(
      { gramProject: "request-project" },
      undefined,
      { enabled: true, throwOnError: false },
    );
    expect(queryHooks.policies).toHaveBeenCalledWith(
      { gramProject: "request-project" },
      undefined,
      { enabled: true, throwOnError: false },
    );
  });

  it.each(["servers", "policies"] as const)(
    "keeps the dashboard and skips the overview when the %s query errors",
    (query) => {
      queryHooks[query].mockReturnValue({
        data: undefined,
        isError: true,
        isPending: false,
      });

      const { result } = renderHook(() => useProjectGuide());

      expect(result.current.status).toBe("dashboard");
      expect(queryHooks.overview).toHaveBeenCalledWith({ enabled: false });
    },
  );

  it("enables the overview and shows the guide only after confirmed empty lists", () => {
    const { result } = renderHook(() => useProjectGuide());

    expect(queryHooks.overview).toHaveBeenCalledWith({ enabled: true });
    expect(result.current.status).toBe("guide");
  });
});
