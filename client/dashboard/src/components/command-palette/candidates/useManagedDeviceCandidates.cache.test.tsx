import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({ requests: 0 }));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-one" }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({}),
}));
vi.mock("@/pages/org/device-integrations/provider-role", () => ({
  isProviderIdVisible: () => true,
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    deviceAgent: { devices: { href: () => "/devices" } },
  }),
}));
vi.mock("react-router", () => ({ useNavigate: () => vi.fn() }));
vi.mock("@gram/client/react-query/fleetDevices.js", () => ({
  buildFleetDevicesInfiniteQuery: () => ({
    queryKey: ["fleetDevices"],
    queryFn: async () => {
      state.requests += 1;
      return {
        result: {
          devices: [
            {
              id: "device-one",
              provider: "jamf",
              hostname: "device-one",
              serialNumber: "SERIAL-one",
            },
          ],
        },
      };
    },
  }),
}));

import { useManagedDeviceCandidates } from "./useManagedDeviceCandidates";

it("reuses the fully loaded device inventory on a quick reopen", async () => {
  state.requests = 0;
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  const { result, rerender, unmount } = renderHook(
    ({ enabled }) => useManagedDeviceCandidates({ enabled }),
    { initialProps: { enabled: true }, wrapper },
  );

  try {
    await waitFor(() => expect(result.current).toHaveLength(1));
    expect(state.requests).toBe(1);

    rerender({ enabled: false });
    rerender({ enabled: true });
    await Promise.resolve();

    expect(result.current).toHaveLength(1);
    expect(state.requests).toBe(1);
  } finally {
    unmount();
    client.clear();
  }
});
