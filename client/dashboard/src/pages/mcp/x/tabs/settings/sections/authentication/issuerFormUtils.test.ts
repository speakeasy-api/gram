import { CreateRemoteSessionClientFormTokenEndpointAuthMethod as AuthMethod } from "@gram/client/models/components/createremotesessionclientform.js";
import { describe, expect, it } from "vitest";
import { clientSecretUpdateValue } from "./issuerFormUtils";

describe("clientSecretUpdateValue", () => {
  it("does not rotate an unsaved secret for private_key_jwt", () => {
    expect(
      clientSecretUpdateValue(AuthMethod.PrivateKeyJwt, "new-secret"),
    ).toBe(undefined);
  });

  it("forwards a new secret for secret-based authentication", () => {
    expect(
      clientSecretUpdateValue(AuthMethod.ClientSecretBasic, " new-secret "),
    ).toBe("new-secret");
    expect(clientSecretUpdateValue(AuthMethod.ClientSecretPost, "  ")).toBe(
      undefined,
    );
  });
});
