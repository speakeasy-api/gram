import { CreateRemoteSessionClientFormTokenEndpointAuthMethod as AuthMethod } from "@gram/client/models/components/createremotesessionclientform.js";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ClientCredentialsFields, OverridesFields } from "./IssuerFormFields";

vi.mock(
  "@gram/client/react-query/newRemoteSessionClientCallbackUrl.js",
  () => ({
    useNewRemoteSessionClientCallbackUrl: () => ({
      data: {
        callbackUrl: "https://new.example.com/mcp/remote_login_callback",
      },
    }),
  }),
);

afterEach(cleanup);

function renderFields(method: AuthMethod, callbackURL?: string): void {
  render(
    <TooltipProvider>
      <ClientCredentialsFields
        clientId="client-1"
        clientSecret=""
        tokenEndpointAuthMethod={method}
        callbackURL={callbackURL}
        allowPrivateKeyJwt
        onClientIdChange={vi.fn<(value: string) => void>()}
        onClientSecretChange={vi.fn<(value: string) => void>()}
        onTokenEndpointAuthMethodChange={vi.fn<
          (value: AuthMethod | "") => void
        >()}
      />
    </TooltipProvider>,
  );
}

describe("ClientCredentialsFields", () => {
  it("hides the secret input for private_key_jwt without suggesting its deletion", () => {
    renderFields(AuthMethod.PrivateKeyJwt);

    expect(screen.queryByText("Client Secret (optional)")).toBeNull();
    expect(
      screen.getByText(/existing client secret is retained but not used/),
    ).toBeTruthy();
  });

  it("shows the secret input for secret-based authentication", () => {
    renderFields(AuthMethod.ClientSecretBasic);

    expect(screen.getByText("Client Secret (optional)")).toBeTruthy();
  });

  it("shows the redirect URI a new client will register", () => {
    renderFields(AuthMethod.ClientSecretBasic);

    expect(
      screen.getByText("https://new.example.com/mcp/remote_login_callback"),
    ).toBeTruthy();
  });

  it("shows the redirect URI an existing client registered", () => {
    renderFields(
      AuthMethod.ClientSecretBasic,
      "https://pinned.example.com/mcp/remote_login_callback",
    );

    expect(
      screen.getByText("https://pinned.example.com/mcp/remote_login_callback"),
    ).toBeTruthy();
  });
});

describe("OverridesFields", () => {
  function renderOverrides(scopeOverride: string): void {
    render(
      <OverridesFields
        scopeOverride={scopeOverride}
        audienceOverride=""
        onScopeOverrideChange={vi.fn<(value: string) => void>()}
        onAudienceOverrideChange={vi.fn<(value: string) => void>()}
        scopeWarning={<div>scope override warning</div>}
      />,
    );
  }

  it("hides the scope warning while the scope field is empty", () => {
    renderOverrides("  ");

    expect(screen.queryByText("scope override warning")).toBeNull();
  });

  it("shows the scope warning once the scope field has text", () => {
    renderOverrides("openid");

    expect(screen.getByText("scope override warning")).toBeTruthy();
  });
});
