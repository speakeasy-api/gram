import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { PropsWithChildren } from "react";
import {
  enterpriseIssuerQueryKey,
  rotationPollInterval,
  useEnterpriseIssuer,
} from "./useEnterpriseIssuer";

const mocks = vi.hoisted(() => ({
  organizationId: "first-org",
  isAdmin: true,
  load: vi.fn(),
  build: vi.fn(),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: mocks.organizationId }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => mocks.isAdmin }),
}));
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
vi.mock("@gram/client/react-query/internalSessionIssuer.core.js", () => ({
  buildInternalSessionIssuerQuery: (...args: unknown[]) => {
    mocks.build(...args);
    return {
      queryKey: ["unscoped"],
      queryFn: mocks.load,
    };
  },
}));
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  mocks.organizationId = "first-org";
  mocks.isAdmin = true;
});

describe("issuer query isolation", () => {
  it("does not show the previous organization's issuer while the next one loads", async () => {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    client.setQueryData(enterpriseIssuerQueryKey("first-org"), {
      issuer: "https://first.example.com",
      state: "enabled",
      keys: [],
      kmsCleanupPending: 0,
      canEnable: false,
    });
    mocks.load.mockImplementation(() => new Promise(() => {}));
    const wrapper = ({ children }: PropsWithChildren) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    );
    const { result, rerender } = renderHook(() => useEnterpriseIssuer(), {
      wrapper,
    });
    expect(mocks.build).toHaveBeenCalledWith({}, undefined, {
      sessionHeaderGramSession: "",
    });
    expect(result.current.data?.issuer).toBe("https://first.example.com");
    mocks.organizationId = "second-org";
    rerender();
    await waitFor(() => expect(result.current.data).toBeUndefined());
    expect(result.current.isPending).toBe(true);
    client.clear();
  });
});

describe("issuer query access", () => {
  it("never reads the issuer for a non-admin", async () => {
    mocks.isAdmin = false;
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const wrapper = ({ children }: PropsWithChildren) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    );
    const { result } = renderHook(() => useEnterpriseIssuer(), { wrapper });
    await waitFor(() => expect(result.current.fetchStatus).toBe("idle"));
    expect(mocks.load).not.toHaveBeenCalled();
    client.clear();
  });
});

describe("rotation polling", () => {
  const now = Date.parse("2026-10-13T12:00:00Z");
  it("polls quickly until ready, then slowly until automatic activation", () => {
    expect(rotationPollInterval(undefined, now)).toBe(false);
    expect(rotationPollInterval(new Date("2026-10-13T12:02:00Z"), now)).toBe(
      5_000,
    );
    expect(rotationPollInterval(new Date("2026-10-13T11:59:59Z"), now)).toBe(
      60_000,
    );
    expect(rotationPollInterval(new Date("2026-10-13T10:00:00Z"), now)).toBe(
      false,
    );
  });
  it("never polls for a readiness time far in the future", () => {
    expect(rotationPollInterval(new Date("2026-10-13T12:30:00Z"), now)).toBe(
      false,
    );
    expect(rotationPollInterval(new Date("not a time"), now)).toBe(false);
  });
});
