import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

import { PrivateNetworkSection } from "./PrivateNetworkSection";

const state = vi.hoisted(() => ({
  isAdmin: true,
  entitled: true,
  productTier: "enterprise" as "enterprise" | "payg" | "base",
  featuresError: false,
  featuresAvailable: true,
  featuresLoading: false,
  ingressError: false,
  ingressPending: false,
  ingressOptions: undefined as
    | {
        refetchInterval?: (query: {
          state: { data?: { ingress?: { status?: string } } };
        }) => number | false;
      }
    | undefined,
  deleteMutate: vi.fn(),
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

vi.mock("@/hooks/useProductTier", () => ({
  useProductTier: () => state.productTier,
}));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-1" }),
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
    data: state.featuresAvailable
      ? { networkIngressEnabled: state.entitled }
      : undefined,
    isLoading: false,
    isPending: state.featuresLoading,
    isError: state.featuresError,
    isSuccess:
      state.featuresAvailable && !state.featuresError && !state.featuresLoading,
  }),
}));

vi.mock("@gram/client/react-query/networkIngress.js", () => ({
  invalidateAllNetworkIngress: vi.fn(),
  useNetworkIngress: (...args: unknown[]) => {
    state.ingressOptions = args[2] as typeof state.ingressOptions;
    return {
      data: { ingress: state.ingress },
      error: state.ingressError ? new Error("unavailable") : null,
      isLoading: false,
      isPending: state.ingressPending,
      isError: state.ingressError,
    };
  },
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
    mutate: state.deleteMutate,
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
  state.isAdmin = true;
  state.entitled = true;
  state.productTier = "enterprise";
  state.featuresError = false;
  state.featuresAvailable = true;
  state.featuresLoading = false;
  state.ingressError = false;
  state.ingressPending = false;
  state.ingressOptions = undefined;
  state.deleteMutate.mockReset();
  state.ingress = undefined;
});

afterEach(cleanup);

describe("PrivateNetworkSection", () => {
  it.each(["base", "payg"] as const)(
    "shows a Tailscale Enterprise upsell for %s without exposing setup",
    (tier) => {
      state.productTier = tier;
      render(<PrivateNetworkSection />);
      expect(screen.getByText("Tailscale private access")).toBeTruthy();
      expect(
        screen.getByRole("link", { name: "Talk to our team" }),
      ).toBeTruthy();
      expect(
        screen.queryByRole("button", { name: "Connect Tailscale" }),
      ).toBeNull();
    },
  );

  it("shows the upsell even when staff enablement is off", () => {
    state.productTier = "payg";
    state.entitled = false;
    render(<PrivateNetworkSection />);
    expect(screen.getByRole("link", { name: "Talk to our team" })).toBeTruthy();
  });

  it("shows the upsell even when feature lookup fails", () => {
    state.productTier = "base";
    state.featuresError = true;
    render(<PrivateNetworkSection />);
    expect(screen.getByRole("link", { name: "Talk to our team" })).toBeTruthy();
  });

  it("shows the enablement message for Enterprise without staff entitlement", () => {
    state.entitled = false;
    render(<PrivateNetworkSection />);
    expect(screen.getByText(/Contact our team to enable it/)).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "Connect Tailscale" }),
    ).toBeNull();
  });

  it("renders no private controls for an organization reader", () => {
    state.isAdmin = false;
    const { container } = render(<PrivateNetworkSection />);
    expect(container.textContent).toBe("");
  });

  it.each(["features", "ingress"] as const)(
    "renders the loading panel while %s are loading",
    (loadingQuery) => {
      if (loadingQuery === "features") {
        state.featuresLoading = true;
      } else {
        state.ingressPending = true;
      }

      render(<PrivateNetworkSection />);

      expect(
        screen.getByText("Loading private network settings..."),
      ).toBeTruthy();
    },
  );

  it("explains that the private hostname is shared across the organization", () => {
    render(<PrivateNetworkSection />);
    fireEvent.click(screen.getByRole("button", { name: "Connect Tailscale" }));

    expect(
      screen.getByRole("textbox", { name: "Organization private hostname" }),
    ).toBeTruthy();
    expect(
      screen.getByText(
        /MCP servers opt in separately and keep their own endpoint paths/,
      ),
    ).toBeTruthy();
  });

  it.each([
    ["failed", true, true],
    ["missing", false, false],
  ] as const)(
    "fails closed when product features are %s",
    (_label, featuresError, featuresAvailable) => {
      state.featuresError = featuresError;
      state.featuresAvailable = featuresAvailable;
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
        screen.getByText(/until both entitlement and ingress checks succeed/),
      ).toBeTruthy();
      expect(screen.queryByRole("button", { name: "Remove" })).toBeNull();
    },
  );

  it("shows and polls pending cleanup instead of clearing the UI", () => {
    state.ingress = {
      id: "ingress-1",
      organizationId: "org-1",
      provider: "tailscale",
      hostname: "private-mcp",
      endpointNamespaceKind: "platform",
      enabled: false,
      identityRequired: false,
      credentialsConfigured: true,
      status: "deleting",
      createdAt: new Date(0),
      updatedAt: new Date(1_000),
    };

    render(<PrivateNetworkSection />);

    expect(screen.getByText("Cleaning up")).toBeTruthy();
    expect(
      screen.getByText(/connect another tailnet after cleanup completes/),
    ).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "Connect Tailscale" }),
    ).toBeNull();
    expect(screen.queryByRole("button", { name: "Remove" })).toBeNull();
    expect(
      state.ingressOptions?.refetchInterval?.({
        state: { data: { ingress: { status: "deleting" } } },
      }),
    ).toBe(5_000);

    fireEvent.click(screen.getByRole("button", { name: "Retry cleanup" }));
    expect(state.deleteMutate).toHaveBeenCalledWith({
      security: { sessionHeaderGramSession: "" },
    });
  });

  it("keeps cached cleanup controls visible when polling fails", () => {
    state.ingressError = true;
    state.ingress = {
      id: "ingress-1",
      organizationId: "org-1",
      provider: "tailscale",
      hostname: "private-mcp",
      endpointNamespaceKind: "platform",
      enabled: false,
      identityRequired: false,
      credentialsConfigured: true,
      status: "deleting",
      createdAt: new Date(0),
      updatedAt: new Date(1_000),
    };

    render(<PrivateNetworkSection />);

    expect(screen.getByText("Cleaning up")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Retry cleanup" })).toBeTruthy();
    expect(
      screen.queryByText(/Private network settings could not be loaded/),
    ).toBeNull();
  });

  it("keeps existing private state visible after plan downgrade", () => {
    state.productTier = "payg";
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
    expect(
      screen
        .getByRole("switch", { name: "Require user identity" })
        .hasAttribute("disabled"),
    ).toBe(true);
    expect(
      screen
        .getByRole("button", { name: "Rotate credentials" })
        .hasAttribute("disabled"),
    ).toBe(true);
    expect(
      screen
        .getByRole("switch", { name: "Private network ingress enabled" })
        .hasAttribute("disabled"),
    ).toBe(false);
  });
});
