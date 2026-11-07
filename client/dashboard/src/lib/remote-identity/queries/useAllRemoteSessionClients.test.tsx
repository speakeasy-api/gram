// @vitest-environment node
import { describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({ query: vi.fn() }));

vi.mock("@gram/client/react-query/remoteSessionClients.js", () => ({
  useRemoteSessionClientsInfinite: () => mocks.query(),
}));
vi.mock("react", async (importOriginal) => ({
  ...(await importOriginal<typeof import("react")>()),
  useEffect: vi.fn(),
  useMemo: (build: () => unknown) => build(),
}));

import { useAllRemoteSessionClients } from "./useAllRemoteSessionClients";

const page = { pages: [{ result: { items: [] } }] };

describe("useAllRemoteSessionClients", () => {
  it.each([
    ["the first page", { isFetching: true }, true, false],
    [
      "a later page",
      { isFetching: true, data: page, hasNextPage: true },
      true,
      false,
    ],
    ["a loaded list", { isFetching: false, data: page }, false, false],
    ["a loaded list refetching", { isFetching: true, data: page }, true, true],
  ])("reports %s", (_, query, isLoading, isRefreshing) => {
    mocks.query.mockReturnValue({ hasNextPage: false, ...query });
    expect(useAllRemoteSessionClients({})).toMatchObject({
      isLoading,
      isRefreshing,
    });
  });
});
