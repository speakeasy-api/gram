import { cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { useIdentitySubject } from "./useIdentitySubject";

const mocks = vi.hoisted(() => ({ get: vi.fn(), identity: vi.fn() }));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org_example" }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({ agents: { get: mocks.get } }),
}));
vi.mock("@gram/client/react-query/identity.js", () => ({
  useIdentity: (...args: unknown[]) => mocks.identity(...args),
}));
afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  mocks.identity.mockReturnValue({ data: undefined });
});
function setup(urn: string) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return renderHook(() => useIdentitySubject(urn), {
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    ),
  });
}
it("loads registered agents through agent authorization without attributing their owner's activity", async () => {
  mocks.get.mockResolvedValue({
    id: "agent_example",
    name: "Release helper",
    ownerUserId: "user_owner",
    ownerProfile: {
      displayName: "Owner",
      photoUrl: "https://example.test/owner.png",
    },
  });
  const { result } = setup("agent:agent_example");
  await waitFor(() =>
    expect(result.current.data?.displayName).toBe("Release helper"),
  );
  expect(mocks.identity).toHaveBeenCalledWith(
    { urn: "agent:agent_example" },
    undefined,
    { enabled: false, throwOnError: false },
  );
  expect(mocks.get).toHaveBeenCalledWith({ id: "agent_example" }, undefined, {
    signal: expect.any(AbortSignal),
  });
  expect(result.current.data).toMatchObject({
    canonicalUrn: "agent:agent_example",
    kind: "agent",
    userIds: [],
    emails: [],
    externalUserIds: [],
  });
  expect(result.current.data?.photoUrl).toBeUndefined();
});
it("keeps human identities on the identity resolver", () => {
  mocks.identity.mockReturnValue({ data: { displayName: "Human" } });
  const { result } = setup("user:user_example");
  expect(result.current.data?.displayName).toBe("Human");
  expect(mocks.identity).toHaveBeenCalledWith(
    { urn: "user:user_example" },
    undefined,
    { enabled: true, throwOnError: false },
  );
  expect(mocks.get).not.toHaveBeenCalled();
});
it("preserves agent lookup failures for the detail page's not-found state", async () => {
  const error = new Error("Not found");
  mocks.get.mockRejectedValue(error);
  const { result } = setup("agent:missing");
  await waitFor(() => expect(result.current.error).toBe(error));
  expect(result.current.data).toBeUndefined();
});
