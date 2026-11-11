import { useEffectiveUserSessionIssuers } from "@/hooks/useEffectiveUserSessionIssuers";
import type { MetaMcpServer } from "@gram/client/models/components/metamcpserver.js";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { useUserSessionIssuer } from "@gram/client/react-query/userSessionIssuer.js";
import { cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useMetaMcpAuthTarget } from "./authTarget";
import { useSelectableUserSessionIssuers } from "./useSelectableUserSessionIssuers";

const { update } = vi.hoisted(() => ({ update: vi.fn() }));

vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({ metaMcp: { update } }),
}));
vi.mock("@/hooks/useEffectiveUserSessionIssuers", () => ({
  useEffectiveUserSessionIssuers: vi.fn(),
}));
vi.mock("@gram/client/react-query/userSessionIssuer.js", () => ({
  useUserSessionIssuer: vi.fn(),
}));

const gateway = {
  id: "gateway-1",
  name: "Shared sign-in gateway",
  projectId: "project-1",
  userSessionIssuerId: "organization-issuer-1",
} as MetaMcpServer;
const organizationIssuer = {
  id: "organization-issuer-1",
  slug: "organization-sign-in",
  projectId: "",
} as UserSessionIssuer;
const projectIssuer = {
  id: "project-issuer-1",
  slug: "project-sign-in",
  projectId: "project-1",
} as UserSessionIssuer;

function useGatewayIssuers() {
  const target = useMetaMcpAuthTarget(gateway, "gateway");
  return { target, ...useSelectableUserSessionIssuers(target) };
}

describe("gateway organization issuer selection", () => {
  beforeEach(() => {
    vi.resetAllMocks();
    vi.mocked(useEffectiveUserSessionIssuers).mockReturnValue({
      issuers: [organizationIssuer],
      organizationIssuers: [organizationIssuer],
      isLoading: false,
      isError: false,
    });
    vi.mocked(useUserSessionIssuer).mockReturnValue({
      data: organizationIssuer,
      isLoading: false,
      isError: false,
    } as never);
  });
  afterEach(cleanup);

  it("keeps an organization-only effective list selectable", () => {
    const { result } = renderHook(useGatewayIssuers);
    expect(result.current.issuers).toEqual([organizationIssuer]);
    expect(result.current.isLoading).toBe(false);
    expect(result.current.isError).toBe(false);
  });

  it("preserves the current organization issuer when absent from the effective list", () => {
    vi.mocked(useEffectiveUserSessionIssuers).mockReturnValue({
      issuers: [projectIssuer],
      organizationIssuers: [],
      isLoading: false,
      isError: false,
    });
    const { result } = renderHook(useGatewayIssuers);
    expect(result.current.issuers).toEqual([organizationIssuer, projectIssuer]);
  });

  it("retains both project and organization choices without duplicating the current issuer", () => {
    vi.mocked(useEffectiveUserSessionIssuers).mockReturnValue({
      issuers: [organizationIssuer, projectIssuer],
      organizationIssuers: [organizationIssuer],
      isLoading: false,
      isError: false,
    });
    const { result } = renderHook(useGatewayIssuers);
    expect(result.current.issuers).toEqual([organizationIssuer, projectIssuer]);
  });

  it("sends the selected organization issuer through the gateway update API", async () => {
    const { result } = renderHook(useGatewayIssuers);
    await result.current.target.linkUserSessionIssuer?.(
      "organization-issuer-2",
    );
    expect(update).toHaveBeenCalledExactlyOnceWith({
      updateMetaMcpServerForm: {
        id: gateway.id,
        name: gateway.name,
        userSessionIssuerId: "organization-issuer-2",
      },
    });
  });
});
