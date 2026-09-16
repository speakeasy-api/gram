import { CreateRemoteSessionClientFormTokenEndpointAuthMethod as AuthMethod } from "@gram/client/models/components/createremotesessionclientform.js";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ClientCredentialsFields } from "./IssuerFormFields";

vi.stubGlobal("__GRAM_SERVER_URL__", "https://gram.example.com");

afterEach(cleanup);

function renderFields(method: AuthMethod): void {
  render(
    <TooltipProvider>
      <ClientCredentialsFields
        clientId="client-1"
        clientSecret=""
        tokenEndpointAuthMethod={method}
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
});
