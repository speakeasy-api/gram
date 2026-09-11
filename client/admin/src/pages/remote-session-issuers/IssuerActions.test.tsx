import { queryKeyAdminGetGlobalIssuer } from "@gram/admin-client/react-query/adminGetGlobalIssuer.core";
import { queryKeyAdminListGlobalIssuerConvergenceCandidates } from "@gram/admin-client/react-query/adminListGlobalIssuerConvergenceCandidates.core";
import { queryKeyAdminListGlobalIssuers } from "@gram/admin-client/react-query/adminListGlobalIssuers.core";
import {
  cleanup,
  render,
  screen,
  fireEvent,
  within,
  waitFor,
} from "@testing-library/react";
import {
  QueryClient,
  QueryClientProvider,
  QueryObserver,
} from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import { IssuerActions } from "./IssuerActions";
import type { GlobalRemoteSessionIssuer } from "@gram/admin-client/models/components/globalremotesessionissuer";
const api = vi.hoisted(() => ({ remove: vi.fn(), refresh: vi.fn() }));
vi.mock("@/lib/gramAdminClient", () => ({
  adminDeleteGlobalIssuer: api.remove,
  adminRefreshGlobalIssuerMetadata: api.refresh,
}));
afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});

it("clears previous refresh warnings while retrying and after failure", async () => {
  const warning = "Previous discovery warning";
  let rejectRefresh!: (reason: Error) => void;
  const pendingRefresh = new Promise<never>((_, reject) => {
    rejectRefresh = reject;
  });
  api.refresh
    .mockResolvedValueOnce({ discoveryWarnings: [warning] })
    .mockReturnValueOnce(pendingRefresh);
  const record = {
    issuer: { id: "target", name: "Example provider" },
    globalClientCount: 0,
    tenantClientCount: 0,
  } as GlobalRemoteSessionIssuer;
  render(
    <QueryClientProvider client={new QueryClient()}>
      <IssuerActions record={record} />
    </QueryClientProvider>,
  );
  const refresh = screen.getByRole("button", { name: "Refresh metadata" });
  fireEvent.click(refresh);
  expect(await screen.findByText(warning)).toBeTruthy();
  await waitFor(() =>
    expect((refresh as HTMLButtonElement).disabled).toBe(false),
  );
  fireEvent.click(refresh);
  expect(api.refresh).toHaveBeenCalledTimes(2);
  expect((refresh as HTMLButtonElement).disabled).toBe(true);
  expect(screen.queryByText(warning)).toBeNull();
  rejectRefresh(new Error("New refresh failure"));
  expect((await screen.findByRole("alert")).textContent).toContain(
    "New refresh failure",
  );
  expect(screen.queryByText(warning)).toBeNull();
});
it("retains deletion errors and identity inside the retryable dialog", async () => {
  api.remove.mockRejectedValue(new Error("Clients appeared"));
  const record = {
    issuer: { id: "target", name: "Example provider" },
    globalClientCount: 0,
    tenantClientCount: 0,
  } as GlobalRemoteSessionIssuer;
  render(
    <QueryClientProvider client={new QueryClient()}>
      <IssuerActions record={record} />
    </QueryClientProvider>,
  );
  fireEvent.click(screen.getByRole("button", { name: "Delete issuer" }));
  const dialog = screen.getByRole("dialog");
  fireEvent.click(
    within(dialog).getByRole("button", { name: "Delete issuer" }),
  );
  expect((await within(dialog).findByRole("alert")).textContent).toContain(
    "Clients appeared",
  );
  expect(within(dialog).getByText(/Example provider/)).toBeTruthy();
  expect(screen.getByRole("dialog")).toBeTruthy();
  api.remove.mockResolvedValueOnce(undefined);
  fireEvent.click(
    within(dialog).getByRole("button", { name: "Delete issuer" }),
  );
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
});

it.each([false, true])(
  "refreshes the list without requesting the deleted issuer (detail navigation: %s)",
  async (navigate) => {
    api.remove.mockResolvedValue(undefined);
    const cache = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    });
    const keys = [
      queryKeyAdminGetGlobalIssuer({ id: "target" }),
      queryKeyAdminListGlobalIssuerConvergenceCandidates({
        targetId: "target",
      }),
      queryKeyAdminListGlobalIssuers({}),
    ];
    const rows = keys.map((queryKey) => {
      cache.setQueryData(queryKey, {});
      const queryFn = vi.fn(async () => ({}));
      const observer = new QueryObserver(cache, { queryKey, queryFn });
      return { queryFn, unsubscribe: observer.subscribe(() => {}) };
    });
    const onDeleted = vi.fn(async () => {});
    const record = {
      issuer: { id: "target", name: "Example provider" },
      globalClientCount: 0,
      tenantClientCount: 0,
    } as GlobalRemoteSessionIssuer;
    try {
      render(
        <QueryClientProvider client={cache}>
          <IssuerActions
            record={record}
            onDeleted={navigate ? onDeleted : undefined}
          />
        </QueryClientProvider>,
      );
      fireEvent.click(screen.getByRole("button", { name: "Delete issuer" }));
      fireEvent.click(
        within(screen.getByRole("dialog")).getByRole("button", {
          name: "Delete issuer",
        }),
      );
      await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
      expect(rows[0]!.queryFn).not.toHaveBeenCalled();
      expect(rows[1]!.queryFn).not.toHaveBeenCalled();
      expect(rows[2]!.queryFn).toHaveBeenCalledTimes(1);
      expect(onDeleted).toHaveBeenCalledTimes(navigate ? 1 : 0);
    } finally {
      cleanup();
      rows.forEach((row) => row.unsubscribe());
      cache.clear();
    }
  },
);
