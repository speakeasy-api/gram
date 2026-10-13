import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { PropsWithChildren } from "react";
import {
  enterpriseIssuerQueryKey,
  useEnterpriseIssuer,
} from "./useEnterpriseIssuer";

const mocks = vi.hoisted(() => ({
  organizationId: "first-org",
  load: vi.fn(),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: mocks.organizationId }),
}));
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
vi.mock("@gram/client/react-query/internalSessionIssuer.core.js", () => ({
  buildInternalSessionIssuerQuery: () => ({
    queryKey: ["unscoped"],
    queryFn: mocks.load,
  }),
}));
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  mocks.organizationId = "first-org";
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
    expect(result.current.data?.issuer).toBe("https://first.example.com");
    mocks.organizationId = "second-org";
    rerender();
    await waitFor(() => expect(result.current.data).toBeUndefined());
    expect(result.current.isPending).toBe(true);
    client.clear();
  });
});
