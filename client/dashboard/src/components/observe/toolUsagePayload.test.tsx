import type { ReactNode } from "react";
import { renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ToolUsageUserFilter } from "@gram/client/models/components/toolusageuserfilter.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import { useIdentityWindow } from "@/pages/identities/useIdentityQueries";
import {
  scopedToolUsageDefaultTypes,
  TOOL_USAGE_VALID_TYPES,
} from "./observeTargetFilters";
import {
  useScopedToolUsagePayload,
  useToolUsagePayload,
} from "./toolUsagePayload";
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
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "default" }),
}));

const agentFilters: ToolUsageUserFilter[] = [
  { kind: "agent_id", key: "agent-1" },
];

/** The inputs the embedded Tool logs derive for a scoped view. */
function useEmbeddedLogsPayload(includeLocalTools: boolean) {
  const filters = useObserveFilters({
    defaultTypes: scopedToolUsageDefaultTypes(includeLocalTools),
    validTypes: TOOL_USAGE_VALID_TYPES,
  });
  return useToolUsagePayload({
    activeFilters: filters.activeFilters,
    roleEmails: filters.roleEmails,
    selectedHookTypes: filters.selectedHookTypes,
    accountType: filters.accountType,
    clientKeys: [],
    agentIds: [],
    scopedUserFilters: agentFilters,
    from: filters.from,
    to: filters.to,
  });
}

function useOverviewPayload(includeLocalTools: boolean) {
  const { from, to } = useIdentityWindow();
  return useScopedToolUsagePayload({
    userFilters: agentFilters,
    includeLocalTools,
    from,
    to,
  });
}

function wrapper(initialUrl: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={qc}>
        <MemoryRouter initialEntries={[initialUrl]}>{children}</MemoryRouter>
      </QueryClientProvider>
    );
  };
}

describe("useScopedToolUsagePayload", () => {
  beforeEach(() => {
    // Mid-minute, so an unaligned window would differ between the two reads.
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2026-11-06T10:15:42.123Z"));
    vi.mocked(useMembers).mockReturnValue({
      data: { members: [] },
      isLoading: false,
    } as unknown as ReturnType<typeof useMembers>);
    vi.mocked(useRoles).mockReturnValue({
      data: { roles: [] },
      isLoading: false,
    } as unknown as ReturnType<typeof useRoles>);
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.clearAllMocks();
  });

  for (const includeLocalTools of [true, false]) {
    it(`asks what an identity's own Tool logs ask (local tools ${includeLocalTools ? "shown" : "hidden"})`, () => {
      const url = includeLocalTools ? "/overview" : "/overview?localTools=hide";
      const logs = renderHook(() => useEmbeddedLogsPayload(includeLocalTools), {
        wrapper: wrapper(url),
      }).result.current;
      vi.setSystemTime(new Date("2026-11-06T10:15:58.900Z"));
      const overview = renderHook(() => useOverviewPayload(includeLocalTools), {
        wrapper: wrapper(url),
      }).result.current;

      expect(JSON.stringify(overview.sharedQueryKey)).toBe(
        JSON.stringify(logs.sharedQueryKey),
      );
      expect(JSON.stringify(overview.summaryPayload)).toBe(
        JSON.stringify(logs.summaryPayload),
      );
      expect(overview.summaryPayload.userFilters).toEqual(agentFilters);
    });
  }

  it("follows the page's custom range exactly", () => {
    const url =
      "/overview?from=2026-11-01T10:00:30.000Z&to=2026-11-02T10:00:30.000Z";
    const overview = renderHook(() => useOverviewPayload(true), {
      wrapper: wrapper(url),
    }).result.current;
    expect(overview.summaryPayload.from.toISOString()).toBe(
      "2026-11-01T10:00:30.000Z",
    );
  });
});
