import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { isSendable } from "./types";

const mocks = vi.hoisted(() => ({
  navigate: vi.fn(),
  query: {
    data: undefined as
      | {
          pages: Array<{
            result: { devices: Array<Record<string, unknown>> };
          }>;
        }
      | undefined,
    hasNextPage: false,
    isFetchingNextPage: false,
    isFetchNextPageError: false,
    fetchNextPage: vi.fn<() => Promise<unknown>>(),
  },
  options: [] as Array<Record<string, unknown>>,
  requests: [] as unknown[],
}));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-one" }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({}),
  useSlugs: () => ({ orgSlug: "org", projectSlug: "project" }),
}));
vi.mock("react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("react-router")>()),
  useNavigate: () => mocks.navigate,
  useLocation: () => ({ pathname: "/" }),
}));
vi.mock("@gram/client/react-query/managedDevices.js", () => ({
  buildManagedDevicesInfiniteQuery: (_sdk: unknown, request: unknown) => {
    mocks.requests.push(request);
    return { queryKey: ["managedDevices"], queryFn: vi.fn() };
  },
}));
vi.mock("@tanstack/react-query", () => ({
  useInfiniteQuery: (options: Record<string, unknown>) => {
    mocks.options.push(options);
    return mocks.query;
  },
}));

import { useManagedDeviceCandidates } from "./useManagedDeviceCandidates";

function device(id: string, overrides: Record<string, unknown> = {}) {
  return {
    id,
    provider: "jamf",
    externalId: `mdm-${id}`,
    hostname: `${id}-mbp`,
    serialNumber: `SERIAL-${id}`,
    userEmail: `${id}@example.com`,
    coverageBucket: "agent_active",
    ...overrides,
  };
}

describe("useManagedDeviceCandidates", () => {
  beforeEach(() => {
    mocks.navigate.mockReset();
    mocks.options.length = 0;
    mocks.requests.length = 0;
    mocks.query.data = undefined;
    mocks.query.hasNextPage = false;
    mocks.query.isFetchingNextPage = false;
    mocks.query.isFetchNextPageError = false;
    mocks.query.fetchNextPage.mockReset().mockResolvedValue(undefined);
  });

  it("does not fetch while closed and scopes the cache to the organization", () => {
    mocks.query.data = { pages: [{ result: { devices: [device("a")] } }] };
    mocks.query.hasNextPage = true;

    const { result } = renderHook(() =>
      useManagedDeviceCandidates({ enabled: false }),
    );

    expect(result.current).toEqual([]);
    expect(mocks.requests.at(-1)).toEqual({ limit: 200 });
    expect(mocks.options.at(-1)).toMatchObject({
      enabled: false,
      retry: false,
      throwOnError: false,
      queryKey: ["managedDevices", "org-one"],
    });
    expect(mocks.query.fetchNextPage).not.toHaveBeenCalled();
  });

  it("finds devices by hostname or serial and keeps them away from the intent service", () => {
    mocks.query.data = {
      pages: [
        { result: { devices: [device("a")] } },
        {
          result: {
            devices: [
              device("b", { hostname: undefined, userEmail: undefined }),
            ],
          },
        },
      ],
    };

    const { result } = renderHook(() =>
      useManagedDeviceCandidates({ enabled: true }),
    );

    expect(result.current.map((candidate) => candidate.title)).toEqual([
      "a-mbp",
      "SERIAL-b",
    ]);
    expect(result.current[0]).toMatchObject({
      id: "managed-device:a",
      detail: "Device · SERIAL-a · a@example.com",
      keywords: ["device", "jamf", "SERIAL-a", "mdm-a", "a@example.com"],
      group: "Devices",
    });
    expect(result.current.every((candidate) => !isSendable(candidate))).toBe(
      true,
    );
  });

  it("opens the device's panel on its integration page", () => {
    const linked = device("a");
    mocks.query.data = { pages: [{ result: { devices: [linked] } }] };

    const { result } = renderHook(() =>
      useManagedDeviceCandidates({ enabled: true }),
    );
    void result.current[0]!.run("open");

    expect(mocks.navigate).toHaveBeenCalledWith(
      "/org/device-agent/mdm-integrations/jamf?device=a",
      { state: { device: linked } },
    );
  });

  it("leaves out devices from providers the dashboard hides", () => {
    mocks.query.data = {
      pages: [
        {
          result: {
            devices: [device("a"), device("b", { provider: "intune" })],
          },
        },
      ],
    };

    const { result } = renderHook(() =>
      useManagedDeviceCandidates({ enabled: true }),
    );

    expect(result.current.map((candidate) => candidate.id)).toEqual([
      "managed-device:a",
    ]);
  });

  it("keeps fetching until every page has loaded, and stops on a failed page", async () => {
    mocks.query.data = { pages: [{ result: { devices: [device("a")] } }] };
    mocks.query.hasNextPage = true;
    const { rerender } = renderHook(() =>
      useManagedDeviceCandidates({ enabled: true }),
    );
    await waitFor(() =>
      expect(mocks.query.fetchNextPage).toHaveBeenCalledTimes(1),
    );

    act(() => {
      mocks.query.isFetchNextPageError = true;
      mocks.query.fetchNextPage.mockClear();
      rerender();
    });
    expect(mocks.query.fetchNextPage).not.toHaveBeenCalled();
  });

  it("resumes after a failed page when the palette opens again", async () => {
    mocks.query.data = { pages: [{ result: { devices: [device("a")] } }] };
    mocks.query.hasNextPage = true;
    const { rerender } = renderHook(
      ({ enabled }) => useManagedDeviceCandidates({ enabled }),
      { initialProps: { enabled: true } },
    );
    await waitFor(() =>
      expect(mocks.query.fetchNextPage).toHaveBeenCalledTimes(1),
    );
    act(() => {
      mocks.query.isFetchNextPageError = true;
      mocks.query.fetchNextPage.mockClear();
      rerender({ enabled: true });
    });
    expect(mocks.query.fetchNextPage).not.toHaveBeenCalled();

    act(() => rerender({ enabled: false }));
    expect(mocks.query.fetchNextPage).not.toHaveBeenCalled();
    act(() => rerender({ enabled: true }));
    expect(mocks.query.fetchNextPage).toHaveBeenCalledTimes(1);
  });
});
