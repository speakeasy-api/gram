import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SettingsTab } from "./SettingsTab";

const mutation = vi.hoisted(() => ({ mutate: vi.fn() }));

vi.mock("@/components/asset-image-upload-field", () => ({
  AssetImageUploadField: () => null,
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@/contexts/Auth", () => ({
  useIsPlatformAdmin: () => false,
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasAnyScope: () => true }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({ remoteIdentityProviders: { goTo: vi.fn() } }),
}));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));
vi.mock("@gram/client/react-query/organizationRemoteSessionIssuer.js", () => ({
  invalidateAllOrganizationRemoteSessionIssuer: vi.fn(),
}));
vi.mock("@gram/client/react-query/organizationRemoteSessionIssuers.js", () => ({
  invalidateAllOrganizationRemoteSessionIssuers: vi.fn(),
}));
vi.mock(
  "@gram/client/react-query/refreshOrganizationRemoteSessionIssuerMetadata.js",
  () => ({
    useRefreshOrganizationRemoteSessionIssuerMetadataMutation: () => ({
      mutate: vi.fn(),
      isPending: false,
    }),
  }),
);
vi.mock(
  "@gram/client/react-query/updateOrganizationRemoteSessionIssuer.js",
  () => ({
    useUpdateOrganizationRemoteSessionIssuerMutation: () => ({
      mutate: mutation.mutate,
      isPending: false,
      error: null,
    }),
  }),
);
vi.mock("../../ExistingIssuerLink", () => ({ ExistingIssuerLink: () => null }));
vi.mock("../../RemoteIdentityProviders", () => ({
  DeleteIssuerDialog: () => null,
}));
vi.mock("./IssuerTunnelSelector", () => ({ IssuerTunnelSelector: () => null }));
vi.mock(
  "../../../mcp/x/tabs/settings/sections/authentication/IssuerFormFields",
  () => ({
    EndpointsFields: () => null,
    IssuerUrlField: () => null,
  }),
);
vi.mock(
  "../../../mcp/x/tabs/settings/sections/authentication/IssuerDuplicateWarning",
  () => ({ IssuerDuplicateWarning: () => null }),
);
vi.mock(
  "../../../mcp/x/tabs/settings/sections/authentication/useIssuerDuplicatePreflight",
  () => ({ useIssuerDuplicatePreflight: () => ({ matches: [] }) }),
);
vi.mock(
  "../../../mcp/x/tabs/settings/sections/authentication/useIssuerDiscovery",
  () => ({
    useIssuerDiscovery: () => ({
      issuerUrl: "https://idp.example.com",
      setIssuerUrl: vi.fn(),
      authorizationEndpoint: "https://idp.example.com/authorize",
      setAuthorizationEndpoint: vi.fn(),
      tokenEndpoint: "https://idp.example.com/token",
      setTokenEndpoint: vi.fn(),
      registrationEndpoint: "",
      setRegistrationEndpoint: vi.fn(),
      jwksUri: "",
      setJwksUri: vi.fn(),
      discoveredSnapshot: null,
      discoverPending: false,
      discoverError: null,
      clearDiscoverError: vi.fn(),
      runDiscover: vi.fn(),
      handleResetEndpoints: vi.fn(),
      resetEndpointState: vi.fn(),
      showDiscoverControls: false,
      showResetControls: false,
      endpointWarnings: [],
    }),
  }),
);

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function issuer(omitScopeFallback?: boolean): RemoteSessionIssuer {
  return {
    id: "issuer-1",
    slug: "example-idp",
    issuer: "https://idp.example.com",
    scopesSupported: ["openid"],
    omitScopeFallback,
  } as RemoteSessionIssuer;
}

function savedForm(): Record<string, unknown> {
  const [call] = mutation.mutate.mock.calls;
  return call![0].request.updateRemoteSessionIssuerForm;
}

describe("issuer settings scope section", () => {
  const toggle = () =>
    screen.getByRole("switch", { name: "Send no scope when nothing is known" });
  const save = () =>
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

  it("seeds the omit-scope-fallback switch from the saved issuer", () => {
    render(<SettingsTab issuer={issuer(true)} />);

    expect(toggle().getAttribute("aria-checked")).toBe("true");
  });

  // An unrelated save must not write false over a NULL the issuer never set.
  it("leaves the switch out of the form when it was not touched", () => {
    render(<SettingsTab issuer={issuer()} />);

    expect(toggle().getAttribute("aria-checked")).toBe("false");
    save();

    expect(savedForm()).toMatchObject({ id: "issuer-1" });
    expect("omitScopeFallback" in savedForm()).toBe(false);
  });

  it("leaves the switch out when it is toggled back to the saved value", () => {
    render(<SettingsTab issuer={issuer()} />);

    fireEvent.click(toggle());
    fireEvent.click(toggle());
    save();

    expect("omitScopeFallback" in savedForm()).toBe(false);
  });

  it("saves the switch on after it is toggled", () => {
    render(<SettingsTab issuer={issuer(false)} />);

    fireEvent.click(toggle());
    save();

    expect(savedForm()).toMatchObject({ omitScopeFallback: true });
  });

  it("saves false when the switch is turned off from on", () => {
    render(<SettingsTab issuer={issuer(true)} />);

    fireEvent.click(toggle());
    expect(toggle().getAttribute("aria-checked")).toBe("false");
    save();

    expect(savedForm()).toMatchObject({ omitScopeFallback: false });
  });
});
