import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  organizationActivityQuery,
  organizationQuery,
  organizationsListQuery,
} from "@/lib/adminQueries";
import {
  TRIAL_STATES,
  type AdminOrganization,
  type ListOrganizationsResult,
} from "@/lib/gramAdminApi";

import {
  canExtendTrial,
  canRearmTrial,
  useDisableOrganization,
  useEnableOrganization,
  useExtendTrial,
  useRearmTrial,
} from "./rowActions";

const mocks = vi.hoisted(() => ({
  disableOrganization:
    vi.fn<(body: { id: string }) => Promise<AdminOrganization>>(),
  enableOrganization:
    vi.fn<(body: { id: string }) => Promise<AdminOrganization>>(),
  extendTrial:
    vi.fn<(body: { id: string; days: number }) => Promise<AdminOrganization>>(),
  rearmTrial:
    vi.fn<(body: { id: string; days: number }) => Promise<AdminOrganization>>(),
}));

vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return {
    ...actual,
    disableOrganization: mocks.disableOrganization,
    enableOrganization: mocks.enableOrganization,
    extendTrial: mocks.extendTrial,
    rearmTrial: mocks.rearmTrial,
  };
});

// Demoted, live and back on the free tier: the one record a re-arm acts on.
const DEMOTED_ORG: AdminOrganization = {
  id: "org_placeholder_one",
  name: "Placeholder One",
  slug: "placeholder-one",
  account_type: "free",
  whitelisted: false,
  trial_state: "demoted",
  member_count: 3,
  created_at: "2026-01-02T00:00:00Z",
  updated_at: "2026-01-07T00:00:00Z",
};

// What the endpoint answers with: the restored account type, the restored
// whitelist flag and a trial running again.
const REARMED_ORG: AdminOrganization = {
  ...DEMOTED_ORG,
  account_type: "enterprise",
  whitelisted: true,
  trial_state: "running",
  trial_ends_at: "2026-08-28T00:00:00Z",
};

beforeEach(() => {
  mocks.disableOrganization.mockReset();
  mocks.disableOrganization.mockResolvedValue(REARMED_ORG);
  mocks.enableOrganization.mockReset();
  mocks.enableOrganization.mockResolvedValue(REARMED_ORG);
  mocks.extendTrial.mockReset();
  mocks.extendTrial.mockResolvedValue(REARMED_ORG);
  mocks.rearmTrial.mockReset();
  mocks.rearmTrial.mockResolvedValue(REARMED_ORG);
});

describe("the trial predicates", () => {
  // Every state the server can send, walked at runtime, and the two actions
  // read together rather than one at a time. Two separate tests would each
  // pass while the menu offered both actions on one record, and a seventh
  // state added later would be walked by neither.
  it.each([...TRIAL_STATES, undefined])(
    "offers at most one of extend and re-arm for the %s trial",
    (state) => {
      const org = { ...DEMOTED_ORG, trial_state: state };

      const offered = [canExtendTrial(org), canRearmTrial(org)].filter(Boolean);

      expect(offered.length).toBeLessThanOrEqual(1);
      // Named as well as counted: "at most one" alone is satisfied by an
      // action that is never offered at all.
      expect(canRearmTrial(org)).toBe(state === "demoted");
    },
  );

  it.each([...TRIAL_STATES, undefined])(
    "keeps re-arm off a disabled organization on the %s trial",
    (state) => {
      // The server would take this request: nothing in the re-arm handler
      // reads disabled_at, so the trial would run behind a lockout. Re-enabling
      // is one press away for an operator who means to make it real.
      expect(
        canRearmTrial({
          ...DEMOTED_ORG,
          trial_state: state,
          disabled_at: "2026-03-04T00:00:00Z",
        }),
      ).toBe(false);
    },
  );
});

describe("audited organization lifecycle mutations", () => {
  function wrapper(qc: QueryClient) {
    return function Wrapper({ children }: { children: ReactNode }) {
      return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
    };
  }

  it.each(["disable", "enable", "extend", "rearm"] as const)(
    "invalidates organization activity after a successful %s",
    async (action) => {
      const qc = new QueryClient({
        defaultOptions: { queries: { retry: false } },
      });
      const activity = organizationActivityQuery(DEMOTED_ORG.id);
      qc.setQueryData(activity.queryKey, {
        pages: [{ logs: [] }],
        pageParams: [undefined],
      });
      const { result } = renderHook(
        () => ({
          disable: useDisableOrganization(),
          enable: useEnableOrganization(),
          extend: useExtendTrial(),
          rearm: useRearmTrial(),
        }),
        { wrapper: wrapper(qc) },
      );

      switch (action) {
        case "disable":
          result.current.disable.mutate(DEMOTED_ORG.id);
          break;
        case "enable":
          result.current.enable.mutate(DEMOTED_ORG.id);
          break;
        case "extend":
          result.current.extend.mutate({ id: DEMOTED_ORG.id, days: 14 });
          break;
        case "rearm":
          result.current.rearm.mutate({ id: DEMOTED_ORG.id, days: 14 });
          break;
      }

      await waitFor(() => {
        expect(result.current[action].isSuccess).toBe(true);
      });
      expect(qc.getQueryState(activity.queryKey)?.isInvalidated).toBe(true);
    },
  );
});

describe("useRearmTrial", () => {
  function wrapper(qc: QueryClient) {
    return function Wrapper({ children }: { children: ReactNode }) {
      return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
    };
  }

  function seededClient(): QueryClient {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    // The list as the operator filtered it: demoted trials only. The re-armed
    // record no longer matches it, which is exactly why this write repaints
    // rather than refetching.
    qc.setQueryData<ListOrganizationsResult>(
      organizationsListQuery({ trial_states: ["demoted"] }).queryKey,
      { organizations: [DEMOTED_ORG] },
    );
    return qc;
  }

  it("repaints the row from the response instead of refetching the list", async () => {
    const qc = seededClient();
    const listKey = organizationsListQuery({
      trial_states: ["demoted"],
    }).queryKey;

    const { result } = renderHook(() => useRearmTrial(), {
      wrapper: wrapper(qc),
    });
    result.current.mutate({ id: DEMOTED_ORG.id, days: 14 });

    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true);
    });
    expect(mocks.rearmTrial).toHaveBeenCalledWith({
      id: DEMOTED_ORG.id,
      days: 14,
    });
    // The record the operator acted on, in its new state, still on the page it
    // was on. An invalidation would leave the old row here and mark the entry
    // stale, and the refetch behind that would drop the row out of a filter it
    // no longer matches, under the operator who just pressed the control.
    expect(qc.getQueryData<ListOrganizationsResult>(listKey)).toEqual({
      organizations: [REARMED_ORG],
    });
    expect(qc.getQueryState(listKey)?.isInvalidated).toBe(false);
  });

  it("writes the record under both keys the detail route can be reached by", async () => {
    const qc = seededClient();

    const { result } = renderHook(() => useRearmTrial(), {
      wrapper: wrapper(qc),
    });
    result.current.mutate({ id: DEMOTED_ORG.id, days: 14 });

    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true);
    });
    expect(qc.getQueryData(organizationQuery(DEMOTED_ORG.id).queryKey)).toEqual(
      REARMED_ORG,
    );
    expect(
      qc.getQueryData(organizationQuery(DEMOTED_ORG.slug).queryKey),
    ).toEqual(REARMED_ORG);
  });

  it("leaves the cached row alone when the write fails", async () => {
    mocks.rearmTrial.mockRejectedValue(new Error("gram admin 409 Conflict"));
    const qc = seededClient();
    const listKey = organizationsListQuery({
      trial_states: ["demoted"],
    }).queryKey;

    const { result } = renderHook(() => useRearmTrial(), {
      wrapper: wrapper(qc),
    });
    result.current.mutate({ id: DEMOTED_ORG.id, days: 14 });

    await waitFor(() => {
      expect(result.current.isError).toBe(true);
    });
    // Nothing was repainted, so the row still says what the server still says.
    expect(qc.getQueryData<ListOrganizationsResult>(listKey)).toEqual({
      organizations: [DEMOTED_ORG],
    });
  });
});
