import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const queryHooks = vi.hoisted(() => ({
  overview: vi.fn(),
  policies: vi.fn(),
  results: vi.fn(),
  servers: vi.fn(),
}));

vi.mock("@/components/project-guide/allTimeOverviewQuery", () => ({
  useAllTimeProjectOverview: queryHooks.overview,
}));
vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "request-project",
}));
vi.mock("@gram/client/react-query/mcpServers.js", () => ({
  useMcpServers: queryHooks.servers,
}));
vi.mock("@gram/client/react-query/riskListPolicies.js", () => ({
  useRiskListPolicies: queryHooks.policies,
}));
vi.mock("@gram/client/react-query/riskListResults.js", () => ({
  useRiskListResults: queryHooks.results,
}));

import { useProjectGuideProgress } from "./useProjectGuideProgress";

beforeEach(() => {
  vi.clearAllMocks();
  queryHooks.servers.mockReturnValue({
    data: { mcpServers: [] },
    isPending: false,
  });
  queryHooks.policies.mockReturnValue({
    data: { policies: [] },
    isPending: false,
  });
  queryHooks.overview.mockReturnValue({ data: undefined, isPending: false });
  queryHooks.results.mockReturnValue({
    data: { results: [] },
    isPending: false,
  });
});

describe("useProjectGuideProgress", () => {
  it("scopes every generated progress query to the request project", () => {
    renderHook(() => useProjectGuideProgress());

    expect(queryHooks.servers).toHaveBeenCalledWith(
      { gramProject: "request-project" },
      undefined,
      { throwOnError: false },
    );
    expect(queryHooks.policies).toHaveBeenCalledWith(
      { gramProject: "request-project" },
      undefined,
      { throwOnError: false },
    );
    expect(queryHooks.results).toHaveBeenCalledWith(
      {
        gramProject: "request-project",
        category: "secrets",
        limit: 1,
      },
      undefined,
      { throwOnError: false },
    );
  });
});
