import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { CreateRemoteSessionClientFormTokenEndpointAuthMethod as AuthMethod } from "@gram/client/models/components/createremotesessionclientform.js";
import { UpdateRemoteSessionClientFormTokenEndpointAuthAudienceFormat as AuthAudienceFormat } from "@gram/client/models/components/updateremotesessionclientform.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { OverviewTab } from "./OverviewTab";

const mutation = vi.hoisted(() => ({ mutate: vi.fn() }));
const auth = vi.hoisted(() => ({ isPlatformAdmin: false, isOrgAdmin: true }));

vi.mock("@/components/ui/Icon", () => ({
  Icon: ({ name }: { name: string }) => <span>{name}</span>,
}));
vi.mock("@/contexts/Auth", () => ({
  useIsPlatformAdmin: () => auth.isPlatformAdmin,
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasAnyScope: (scopes: string[]) =>
      auth.isOrgAdmin || !scopes.includes("org:admin"),
    hasScope: () => auth.isOrgAdmin,
    hasAllScopes: () => auth.isOrgAdmin,
    isLoading: false,
  }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    remoteIdentityProviders: {
      issuerDetail: {
        goTo: vi.fn(),
        overview: { href: (id: string) => `/rip/${id}/overview` },
        settings: { href: (id: string) => `/rip/${id}/settings` },
      },
    },
  }),
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({
    children,
    scope,
  }: {
    children: ReactNode;
    scope: string | string[];
  }) => (
    <div
      data-required-scope={String(scope)}
      data-allowed={String(auth.isOrgAdmin)}
    >
      {children}
    </div>
  ),
}));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));
vi.mock(
  "@gram/client/react-query/updateOrganizationRemoteSessionClient.js",
  () => ({
    useUpdateOrganizationRemoteSessionClientMutation: () => ({
      mutate: mutation.mutate,
      isPending: false,
    }),
  }),
);
vi.mock("./KeySetField", () => ({ KeySetField: () => <div>key set</div> }));
vi.mock("../../clientDialogs", () => ({
  DeleteClientDialog: () => <div>delete dialog</div>,
  RotateClientDialog: () => <div>rotate dialog</div>,
}));
vi.mock(
  "../../../mcp/x/tabs/settings/sections/authentication/IssuerFormFields",
  () => ({
    ClientAssertionAudienceField: ({
      value,
      onChange,
      disabled,
    }: {
      value: AuthAudienceFormat;
      onChange: (value: AuthAudienceFormat) => void;
      disabled?: boolean;
    }) => (
      <select
        aria-label="Client assertion audience"
        value={value}
        disabled={disabled}
        onChange={(event) => onChange(event.target.value as AuthAudienceFormat)}
      >
        <option value={AuthAudienceFormat.Issuer}>Issuer URL (default)</option>
        <option value={AuthAudienceFormat.TokenEndpoint}>
          Token endpoint URL
        </option>
      </select>
    ),
    TokenEndpointAuthMethodField: ({
      value,
      onChange,
      allowPrivateKeyJwt,
      disabled,
    }: {
      value: AuthMethod | "";
      onChange: (method: AuthMethod | "") => void;
      allowPrivateKeyJwt?: boolean;
      disabled?: boolean;
    }) => (
      <select
        aria-label="Authentication method"
        value={value}
        disabled={disabled}
        onChange={(event) => onChange(event.target.value as AuthMethod)}
      >
        <option value={AuthMethod.ClientSecretBasic}>
          client_secret_basic
        </option>
        {(allowPrivateKeyJwt || value === AuthMethod.PrivateKeyJwt) && (
          <option value={AuthMethod.PrivateKeyJwt}>private_key_jwt</option>
        )}
      </select>
    ),
  }),
);

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  auth.isPlatformAdmin = false;
  auth.isOrgAdmin = true;
});

function client(
  method: AuthMethod = AuthMethod.ClientSecretBasic,
  overrides: Partial<RemoteSessionClient> = {},
): RemoteSessionClient {
  return {
    id: "client-1",
    clientId: "upstream-client",
    tokenEndpointAuthMethod: method,
    jsonWebKeySetId: "set-1",
    legacyCallbackUrl: false,
    ...overrides,
  } as RemoteSessionClient;
}

function issuer(
  overrides: Partial<RemoteSessionIssuer> = {},
): RemoteSessionIssuer {
  return {
    id: "issuer-1",
    name: "Example IdP",
    issuer: "https://idp.example.com",
    organizationId: "org-1",
    projectId: "",
    authorizationEndpoint: "https://idp.example.com/authorize",
    tokenEndpoint: "https://idp.example.com/token",
    registrationEndpoint: "https://idp.example.com/register",
    clientIdMetadataDocumentSupported: true,
    tokenEndpointAuthMethodsSupported: ["client_secret_basic", "none"],
    scopesSupported: ["openid", "profile"],
    ...overrides,
  } as RemoteSessionIssuer;
}

function renderTab(
  props: {
    client?: RemoteSessionClient;
    issuer?: RemoteSessionIssuer;
    isIssuerLoading?: boolean;
  } = {},
) {
  const ui = (next: typeof props) => (
    <MemoryRouter>
      <OverviewTab
        client={next.client ?? client()}
        issuer={"issuer" in next ? next.issuer : issuer()}
        isIssuerLoading={next.isIssuerLoading ?? false}
        issuerId="issuer-1"
      />
    </MemoryRouter>
  );
  const view = render(ui(props));
  return {
    ...view,
    rerenderWith: (next: typeof props) => view.rerender(ui(next)),
  };
}

function field(label: string): HTMLElement {
  return screen.getByText(label).parentElement!;
}

function lastSaved(): Record<string, unknown> {
  const call = mutation.mutate.mock.lastCall![0] as {
    request: { updateRemoteSessionClientForm: Record<string, unknown> };
  };
  return call.request.updateRemoteSessionClientForm;
}

// A disabled fieldset disables its controls; jsdom does not apply that to
// :disabled, so check it the way the browser does.
function isDisabled(control: Element): boolean {
  return (
    (control as HTMLInputElement).disabled ||
    control.closest("fieldset")?.disabled === true
  );
}

function save() {
  fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
}

function scopesField(): HTMLElement {
  return screen.getByRole("combobox", { name: "Scopes" });
}

function openScopes() {
  fireEvent.click(screen.getByText("No scopes"));
}

describe("client overview identity provider card", () => {
  it("shows the issuer's metadata and links to its page", () => {
    renderTab();

    const link = screen.getByRole("link", { name: "Example IdP" });
    expect(link.getAttribute("href")).toBe("/rip/issuer-1/overview");
    expect(field("Issuer").textContent).toContain("https://idp.example.com");
    expect(field("Authorization Endpoint").textContent).toContain(
      "https://idp.example.com/authorize",
    );
    expect(field("Token Endpoint").textContent).toContain(
      "https://idp.example.com/token",
    );
    expect(field("Registration Endpoint").textContent).toContain(
      "https://idp.example.com/register",
    );
    expect(field("Registration Methods").textContent).toContain(
      "Dynamic client registration, Client ID metadata document",
    );
    expect(
      field("Token Endpoint Authentication Methods").textContent,
    ).toContain("client_secret_basic, none");
    expect(field("Scopes Supported").textContent).toContain("openid, profile");
  });

  it("reads none advertised when the issuer offers no registration", () => {
    renderTab({
      issuer: issuer({
        registrationEndpoint: undefined,
        clientIdMetadataDocumentSupported: false,
      }),
    });

    expect(field("Registration Methods").textContent).toContain(
      "None advertised",
    );
    expect(field("Registration Endpoint").textContent).toContain("—");
  });

  it("omits the card when the issuer failed to load", () => {
    renderTab({ issuer: undefined });

    expect(screen.queryByText("Identity Provider")).toBeNull();
    expect(screen.getByText("Client")).toBeTruthy();
  });

  it("keeps the card while the issuer loads", () => {
    renderTab({ issuer: undefined, isIssuerLoading: true });

    expect(screen.getByText("Identity Provider")).toBeTruthy();
    expect(screen.queryByText("Issuer")).toBeNull();
  });
});

describe("client overview scope picker", () => {
  it("offers the issuer's scopes and saves the selection as an array", () => {
    renderTab();

    openScopes();
    expect(screen.getByRole("option", { name: /openid/ })).toBeTruthy();
    expect(screen.getByRole("option", { name: /profile/ })).toBeTruthy();
    fireEvent.click(screen.getByRole("option", { name: /openid/ }));
    save();

    expect(lastSaved().scope).toEqual(["openid"]);
  });

  it("adds a typed scope the issuer does not advertise", () => {
    renderTab();

    openScopes();
    fireEvent.change(screen.getByPlaceholderText("Search options..."), {
      target: { value: "Custom.Read" },
    });
    fireEvent.click(screen.getByRole("option", { name: /Create new option/ }));
    save();

    expect(lastSaved().scope).toEqual(["Custom.Read"]);
  });

  it("seeds saved scopes, including ones the issuer does not advertise", () => {
    renderTab({ client: client(undefined, { scope: ["openid", "extra"] }) });
    save();

    expect(lastSaved().scope).toEqual(["openid", "extra"]);
    expect(screen.getByText("extra")).toBeTruthy();
  });

  it("sends an empty array when no scopes are chosen", () => {
    renderTab();
    save();

    expect(lastSaved().scope).toEqual([]);
  });

  it("warns about an issuer override only once a scope is selected", () => {
    renderTab({ issuer: issuer({ scopeOverride: ["read"] }) });

    expect(screen.queryByText(/has a scope override/)).toBeNull();
    openScopes();
    fireEvent.click(screen.getByRole("option", { name: /openid/ }));
    expect(screen.getByText(/has a scope override/)).toBeTruthy();
  });

  it("does not warn when the issuer has no override", () => {
    renderTab({ client: client(undefined, { scope: ["openid"] }) });

    expect(screen.queryByText(/has a scope override/)).toBeNull();
  });
});

describe("client overview read-only access", () => {
  it("disables every field and hides Save without org:admin", () => {
    auth.isOrgAdmin = false;
    const { container } = renderTab({
      client: client(undefined, { scope: ["openid"], audience: "aud" }),
    });

    expect(screen.queryByRole("button", { name: "Save changes" })).toBeNull();
    const controls = [
      ...container.querySelectorAll("fieldset input, fieldset select"),
    ];
    expect(controls.length).toBeGreaterThan(0);
    for (const control of controls) {
      expect(isDisabled(control)).toBe(true);
    }
    expect(isDisabled(screen.getByRole("combobox", { name: "Scopes" }))).toBe(
      true,
    );
    expect(
      screen
        .getByText("key set")
        .closest("[data-required-scope]")!
        .getAttribute("data-required-scope"),
    ).toBe("org:admin");
    expect(
      screen
        .getByRole("button", { name: "Delete client" })
        .closest("[data-required-scope]")!
        .getAttribute("data-required-scope"),
    ).toBe("org:admin");
  });

  it("hides secret rotation without org:admin", () => {
    auth.isOrgAdmin = false;
    renderTab();

    expect(screen.queryByLabelText("Rotate Client Secret")).toBeNull();
    expect(screen.queryByText(/encrypted at rest/)).toBeNull();
  });

  it("shows refetched values rather than a stale draft without org:admin", () => {
    auth.isOrgAdmin = false;
    const view = renderTab({
      client: client(undefined, { scope: ["openid"], audience: "old-aud" }),
    });

    view.rerenderWith({
      client: client(AuthMethod.PrivateKeyJwt, {
        scope: ["profile"],
        audience: "new-aud",
        tokenEndpointAuthAudienceFormat: AuthAudienceFormat.TokenEndpoint,
      }),
    });

    expect((screen.getByLabelText("Audience") as HTMLInputElement).value).toBe(
      "new-aud",
    );
    const scopes = scopesField().parentElement!.textContent;
    expect(scopes).toContain("profile");
    expect(scopes).not.toContain("openid");
    expect(
      (
        screen.getByRole("combobox", {
          name: "Authentication method",
        }) as HTMLSelectElement
      ).value,
    ).toBe(AuthMethod.PrivateKeyJwt);
    expect(
      (
        screen.getByRole("combobox", {
          name: "Client assertion audience",
        }) as HTMLSelectElement
      ).value,
    ).toBe(AuthAudienceFormat.TokenEndpoint);
  });

  it("enables fields and Save for org admins", () => {
    renderTab();

    expect(screen.getByRole("button", { name: "Save changes" })).toBeTruthy();
    expect(
      isDisabled(
        screen.getByRole("combobox", { name: "Authentication method" }),
      ),
    ).toBe(false);
  });
});

describe("client overview settings", () => {
  it("shows the client ID read-only at the top of the card", () => {
    renderTab();

    expect(field("Client ID").textContent).toContain("upstream-client");
  });

  it("saves a trimmed audience", () => {
    renderTab();

    fireEvent.change(screen.getByLabelText("Audience"), {
      target: { value: "  api://example  " },
    });
    save();

    expect(lastSaved().audience).toBe("api://example");
  });

  it("clears the audience when the field is emptied", () => {
    renderTab({ client: client(undefined, { audience: "old-aud" }) });

    fireEvent.change(screen.getByLabelText("Audience"), {
      target: { value: "  " },
    });
    save();

    expect(lastSaved().audience).toBe("");
  });

  it("hides secret rotation when private_key_jwt is selected", () => {
    renderTab({ client: client(AuthMethod.PrivateKeyJwt) });

    expect(screen.queryByText("Rotate Client Secret")).toBeNull();
    expect(
      screen.getByText(/existing client secret is retained but not used/),
    ).toBeTruthy();
  });

  it("sends a new client secret", () => {
    renderTab();

    fireEvent.change(screen.getByLabelText("Rotate Client Secret"), {
      target: { value: "new-secret" },
    });
    save();

    expect(lastSaved().clientSecret).toBe("new-secret");
  });

  it("clears and omits an unsaved secret after switching to private_key_jwt", () => {
    renderTab();

    fireEvent.change(screen.getByLabelText("Rotate Client Secret"), {
      target: { value: "staged-secret" },
    });
    fireEvent.change(
      screen.getByRole("combobox", { name: "Authentication method" }),
      { target: { value: AuthMethod.PrivateKeyJwt } },
    );

    expect(screen.queryByText("Rotate Client Secret")).toBeNull();
    save();
    expect(lastSaved()).toEqual(
      expect.objectContaining({
        id: "client-1",
        tokenEndpointAuthMethod: AuthMethod.PrivateKeyJwt,
        clientSecret: undefined,
        tokenEndpointAuthAudienceFormat: undefined,
      }),
    );

    fireEvent.change(
      screen.getByRole("combobox", { name: "Authentication method" }),
      { target: { value: AuthMethod.ClientSecretBasic } },
    );
    expect(
      (screen.getByLabelText("Rotate Client Secret") as HTMLInputElement).value,
    ).toBe("");
  });

  it("hides private_key_jwt when no signing key set is attached", () => {
    renderTab({ client: client(undefined, { jsonWebKeySetId: undefined }) });

    expect(
      screen.queryByRole("option", { name: AuthMethod.PrivateKeyJwt }),
    ).toBeNull();
  });

  it("discards an unsaved private_key_jwt selection when the key set is detached", () => {
    const view = renderTab();
    fireEvent.change(
      screen.getByRole("combobox", { name: "Authentication method" }),
      { target: { value: AuthMethod.PrivateKeyJwt } },
    );

    view.rerenderWith({
      client: client(undefined, { jsonWebKeySetId: undefined }),
    });

    expect(
      (
        screen.getByRole("combobox", {
          name: "Authentication method",
        }) as HTMLSelectElement
      ).value,
    ).toBe(AuthMethod.ClientSecretBasic);
  });

  it("preserves an unset assertion audience on an unrelated save", () => {
    renderTab();
    save();

    expect(lastSaved().tokenEndpointAuthAudienceFormat).toBeUndefined();
  });

  it("saves an explicitly selected token endpoint audience", () => {
    renderTab({ client: client(AuthMethod.PrivateKeyJwt) });

    fireEvent.change(
      screen.getByRole("combobox", { name: "Client assertion audience" }),
      { target: { value: AuthAudienceFormat.TokenEndpoint } },
    );
    save();

    expect(lastSaved().tokenEndpointAuthAudienceFormat).toBe(
      AuthAudienceFormat.TokenEndpoint,
    );
  });

  it("hides the legacy callback switch from everyone but platform admins", () => {
    renderTab();

    expect(screen.queryByRole("switch")).toBeNull();
  });

  it("sends the legacy callback flag only when a platform admin changes it", () => {
    auth.isPlatformAdmin = true;
    renderTab({ client: client(undefined, { legacyCallbackUrl: true }) });

    const toggle = screen.getByRole("switch");
    expect(toggle.getAttribute("aria-checked")).toBe("true");

    save();
    expect(lastSaved().legacyCallbackUrl).toBeUndefined();

    fireEvent.click(toggle);
    save();
    expect(lastSaved().legacyCallbackUrl).toBe(false);
  });
});

describe("client overview registration and danger zone", () => {
  it("offers rotation when the issuer publishes a registration endpoint", () => {
    renderTab();

    fireEvent.click(screen.getByRole("button", { name: "Rotate client" }));
    expect(screen.getByText("rotate dialog")).toBeTruthy();
  });

  it("offers no rotation without a registration endpoint", () => {
    renderTab({ issuer: issuer({ registrationEndpoint: undefined }) });

    expect(screen.queryByRole("button", { name: "Rotate client" })).toBeNull();
    expect(screen.getByText(/publishes no registration endpoint/)).toBeTruthy();
  });

  it("opens the delete dialog", () => {
    renderTab();

    fireEvent.click(screen.getByRole("button", { name: "Delete client" }));
    expect(screen.getByText("delete dialog")).toBeTruthy();
  });
});
