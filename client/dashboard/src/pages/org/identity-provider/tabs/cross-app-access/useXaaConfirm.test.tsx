import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { OktaResourceConnectionServer } from "@gram/client/models/components/oktaresourceconnectionserver.js";

import { useXaaConfirm } from "./useXaaConfirm";
import { pendingRow } from "./xaaTestRows";

const mocks = vi.hoisted(() => ({ confirm: vi.fn(), invalidate: vi.fn() }));
vi.mock("@gram/client/react-query/confirmOktaResourceConnections.js", () => ({
  useConfirmOktaResourceConnectionsMutation: () => ({
    mutateAsync: mocks.confirm,
  }),
}));
vi.mock("../../identityProviderQueries", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../identityProviderQueries")>()),
  invalidateIdentityProviderQueries: mocks.invalidate,
}));
vi.mock("./xaaView", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./xaaView")>();
  return {
    ...actual,
    buildConfirmRequests: (
      rows: { mcpServerId: string }[],
      audience: string,
      appId: string | undefined,
    ) => actual.buildConfirmRequests(rows, audience, appId, 2),
  };
});

const AUDIENCE = "https://issuer.example.com";
const rows = [pendingRow(0), pendingRow(1), pendingRow(2)];

function renderConfirm() {
  const onBatchConfirmed =
    vi.fn<(rows: OktaResourceConnectionServer[]) => void>();
  const client = new QueryClient();
  const hook = renderHook(() => useXaaConfirm(onBatchConfirmed), {
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    ),
  });
  return { ...hook, onBatchConfirmed };
}

function sentConnections(call: number) {
  return mocks.confirm.mock.calls[call]?.[0].request
    .confirmOktaResourceConnectionsRequestBody.connections;
}

beforeEach(() => vi.resetAllMocks());
afterEach(cleanup);

describe("useXaaConfirm", () => {
  it("sends one request per batch and counts a shared server once", async () => {
    mocks.confirm
      .mockResolvedValueOnce({ servers: rows.slice(0, 2) })
      .mockResolvedValueOnce({ servers: rows.slice(1) });
    const { result, onBatchConfirmed } = renderConfirm();
    let outcome;
    await act(async () => {
      outcome = await result.current.confirmRows(rows, AUDIENCE, "app");
    });
    expect(outcome).toEqual({ ok: true, count: 3 });
    expect(result.current.feedback).toEqual({ kind: "confirmed", count: 3 });
    expect(sentConnections(0)).toEqual([
      { mcpServerId: "server-0", audience: AUDIENCE, oktaApplicationId: "app" },
      { mcpServerId: "server-1", audience: AUDIENCE, oktaApplicationId: "app" },
    ]);
    expect(sentConnections(1)).toEqual([
      { mcpServerId: "server-2", audience: AUDIENCE, oktaApplicationId: "app" },
    ]);
    expect(onBatchConfirmed.mock.calls).toEqual([
      [rows.slice(0, 2)],
      [rows.slice(2)],
    ]);
    expect(mocks.invalidate).toHaveBeenCalledTimes(1);
    expect(result.current.confirming).toBe(false);
  });

  it("reports the batches that completed before a failure", async () => {
    const error = new Error("Later batch failed");
    mocks.confirm
      .mockResolvedValueOnce({ servers: rows.slice(0, 2) })
      .mockRejectedValueOnce(error);
    const { result, onBatchConfirmed } = renderConfirm();
    let outcome;
    await act(async () => {
      outcome = await result.current.confirmRows(rows, AUDIENCE, undefined);
    });
    expect(outcome).toEqual({
      ok: false,
      confirmedIds: new Set(["server-0", "server-1"]),
    });
    expect(result.current.feedback).toEqual({
      kind: "failed",
      error,
      confirmedCount: 2,
    });
    expect(onBatchConfirmed).toHaveBeenCalledTimes(1);
    expect(mocks.invalidate).toHaveBeenCalledTimes(1);
    act(() => result.current.clearFeedback());
    expect(result.current.feedback).toEqual({ kind: "idle" });
  });

  it("ignores a second confirmation while one is running", async () => {
    let resolve!: (value: { servers: OktaResourceConnectionServer[] }) => void;
    mocks.confirm.mockImplementation(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    );
    const { result } = renderConfirm();
    let first!: Promise<unknown>;
    act(() => {
      first = result.current.confirmRows(rows.slice(0, 1), AUDIENCE, undefined);
    });
    expect(result.current.confirming).toBe(true);
    await expect(
      result.current.confirmRows(rows.slice(1, 2), AUDIENCE, undefined),
    ).resolves.toBeUndefined();
    await act(async () => {
      resolve({ servers: rows.slice(0, 1) });
      await first;
    });
    expect(mocks.confirm).toHaveBeenCalledTimes(1);
  });

  it("does nothing for an empty selection", async () => {
    const { result } = renderConfirm();
    await expect(
      result.current.confirmRows([], AUDIENCE, undefined),
    ).resolves.toBeUndefined();
    expect(mocks.confirm).not.toHaveBeenCalled();
    expect(mocks.invalidate).not.toHaveBeenCalled();
  });
});
