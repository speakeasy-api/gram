import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";

import { PrivateNetworkSection } from "./PrivateNetworkSection";

const state = vi.hoisted(() => ({
  rolloutStatus: "enabled" as
    | "loading"
    | "enabled"
    | "disabled"
    | "missing"
    | "error",
  isAdmin: true,
  entitled: true,
  ingress: undefined as
    | {
        id: string;
        organizationId: string;
        provider: "tailscale";
        hostname: string;
        endpointNamespaceKind: "platform";
        enabled: boolean;
        identityRequired: boolean;
        credentialsConfigured: boolean;
        status: string;
        createdAt: Date;
        updatedAt: Date;
      }
    | undefined,
}));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-1" }),
}));

vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => ({ status: state.rolloutStatus }),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: () => state.isAdmin,
    hasAnyScope: () => state.isAdmin,
    hasAllScopes: () => state.isAdmin,
  }),
}));

vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  useProductFeatures: () => ({
    data: { networkIngressEnabled: state.entitled },
    isLoading: false,
  }),
}));

vi.mock("@gram/client/react-query/networkIngress.js", () => ({
  invalidateAllNetworkIngress: vi.fn(),
  useNetworkIngress: () => ({
    data: { ingress: state.ingress },
    error: null,
    isLoading: false,
  }),
}));

vi.mock("@gram/client/react-query/createNetworkIngress.js", () => ({
  useCreateNetworkIngressMutation: () => ({
    isPending: false,
    mutate: vi.fn(),
  }),
}));
vi.mock("@gram/client/react-query/updateNetworkIngress.js", () => ({
  useUpdateNetworkIngressMutation: () => ({
    isPending: false,
    mutate: vi.fn(),
  }),
}));
vi.mock("@gram/client/react-query/rotateNetworkIngressCredentials.js", () => ({
  useRotateNetworkIngressCredentialsMutation: () => ({
    isPending: false,
    mutate: vi.fn(),
  }),
}));
vi.mock("@gram/client/react-query/networkIngressDeleteIngress.js", () => ({
  useNetworkIngressDeleteIngressMutation: () => ({
    isPending: false,
    mutate: vi.fn(),
  }),
}));
vi.mock("@gram/client/react-query/networkIngressCheckHealth.js", () => ({
  useNetworkIngressCheckHealthMutation: () => ({
    isPending: false,
    mutate: vi.fn(),
  }),
}));
vi.mock("@gram/client/react-query/networkIngressGetDeleteImpact.js", () => ({
  useNetworkIngressGetDeleteImpact: () => ({
    data: undefined,
    isLoading: false,
    isError: false,
  }),
}));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));
vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

beforeEach(() => {
  state.rolloutStatus = "enabled";
  state.isAdmin = true;
  state.entitled = true;
  state.ingress = undefined;
});

afterEach(cleanup);

describe("PrivateNetworkSection", () => {
  it.each(["loading", "disabled", "missing", "error"] as const)(
    "renders no private controls when rollout is %s",
    (rolloutStatus) => {
      state.rolloutStatus = rolloutStatus;
      const { container } = render(<PrivateNetworkSection />);
      expect(container.textContent).toBe("");
    },
  );

  it("renders no private controls for an organization reader", () => {
    state.isAdmin = false;
    const { container } = render(<PrivateNetworkSection />);
    expect(container.textContent).toBe("");
  });

  it("shows setup only for an entitled admin", () => {
    render(<PrivateNetworkSection />);
    expect(
      screen.getByRole("button", { name: "Connect Tailscale" }),
    ).toBeTruthy();
  });

  it("does not show setup without entitlement", () => {
    state.entitled = false;
    render(<PrivateNetworkSection />);
    expect(
      screen.queryByRole("button", { name: "Connect Tailscale" }),
    ).toBeNull();
    expect(
      screen.getByText(
        "Private network access is not enabled for this organization.",
      ),
    ).toBeTruthy();
  });

  it("keeps existing private state visible after entitlement removal", () => {
    state.entitled = false;
    state.ingress = {
      id: "ingress-1",
      organizationId: "org-1",
      provider: "tailscale",
      hostname: "private-mcp",
      endpointNamespaceKind: "platform",
      enabled: true,
      identityRequired: false,
      credentialsConfigured: true,
      status: "online",
      createdAt: new Date(0),
      updatedAt: new Date(0),
    };

    render(<PrivateNetworkSection />);
    expect(
      screen.getByText(/Existing restrictions remain enforced/),
    ).toBeTruthy();
    expect(screen.getByRole("button", { name: "Remove" })).toBeTruthy();
  });
});
