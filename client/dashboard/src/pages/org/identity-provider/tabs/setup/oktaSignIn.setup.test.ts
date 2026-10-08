import type { GramCore } from "@gram/client/core.js";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { isSignInClientReady, setUpOktaSignIn } from "./oktaSignIn";

const api = vi.hoisted(() => ({
  createSet: vi.fn(),
  listKeys: vi.fn(),
  publishKey: vi.fn(),
  attachKeySet: vi.fn(),
  createClient: vi.fn(),
  updateClient: vi.fn(),
  createIssuer: vi.fn(),
  updateIssuer: vi.fn(),
}));

vi.mock("@gram/client/funcs/jsonWebKeySetsCreate.js", () => ({
  jsonWebKeySetsCreate: api.createSet,
}));
vi.mock("@gram/client/funcs/jsonWebKeySetsListKeys.js", () => ({
  jsonWebKeySetsListKeys: api.listKeys,
}));
vi.mock("@gram/client/funcs/jsonWebKeySetsPublishKey.js", () => ({
  jsonWebKeySetsPublishKey: api.publishKey,
}));
vi.mock(
  "@gram/client/funcs/organizationRemoteSessionClientsAttachKeySet.js",
  () => ({ organizationRemoteSessionClientsAttachKeySet: api.attachKeySet }),
);
vi.mock("@gram/client/funcs/organizationRemoteSessionClientsCreate.js", () => ({
  organizationRemoteSessionClientsCreate: api.createClient,
}));
vi.mock("@gram/client/funcs/organizationRemoteSessionClientsUpdate.js", () => ({
  organizationRemoteSessionClientsUpdate: api.updateClient,
}));
vi.mock("@gram/client/funcs/organizationUserSessionIssuersCreate.js", () => ({
  organizationUserSessionIssuersCreate: api.createIssuer,
}));
vi.mock("@gram/client/funcs/organizationUserSessionIssuersUpdate.js", () => ({
  organizationUserSessionIssuersUpdate: api.updateIssuer,
}));

const ok = <T>(value: T) => Promise.resolve({ ok: true, value });
const fail = (error: Error) => Promise.resolve({ ok: false, error });
const core = {} as GramCore;

function makeClient(
  overrides: Partial<RemoteSessionClient> = {},
): RemoteSessionClient {
  return {
    id: "client",
    clientId: "agent-id",
    createdAt: new Date("2026-01-01T00:00:00Z"),
    updatedAt: new Date("2026-01-01T00:00:00Z"),
    grantTypes: null,
    legacyCallbackUrl: false,
    organizationId: "organization-id",
    projectId: "",
    remoteSessionIssuerId: "remote-issuer-id",
    userSessionIssuerIds: [],
    ...overrides,
  };
}

const issuer = {
  id: "issuer",
  slug: "okta-sign-in",
  trustedRemoteSessionIssuerId: "remote-issuer-id",
  trustedRemoteSessionClientId: "client",
} as UserSessionIssuer;

const baseInput = {
  remoteSessionIssuerId: "remote-issuer-id",
  agentId: "agent-id",
  newIssuerSlug: "okta-sign-in",
  userSessionIssuer: issuer,
};

beforeEach(() => {
  for (const fn of Object.values(api)) fn.mockReset();
  api.listKeys.mockReturnValue(ok({ keys: [{ keyState: "active" }] }));
});

describe("setUpOktaSignIn", () => {
  it("reports a created key set so a retry reuses it instead of creating another", async () => {
    api.createSet.mockReturnValue(ok({ id: "new-set" }));
    api.attachKeySet.mockReturnValue(fail(new Error("attach failed")));
    const onKeySetCreated = vi.fn<(setId: string) => void>();

    await expect(
      setUpOktaSignIn(core, {
        ...baseInput,
        existingClient: makeClient(),
        keySet: { kind: "create", externalKeyId: "kms-key" },
        onKeySetCreated,
      }),
    ).rejects.toThrow("attach failed");
    expect(onKeySetCreated).toHaveBeenCalledWith("new-set");

    api.attachKeySet.mockReturnValue(
      ok(
        makeClient({
          jsonWebKeySetId: "new-set",
          tokenEndpointAuthMethod: "private_key_jwt",
          tokenEndpointAuthAudienceFormat: "token_endpoint",
          scope: ["openid", "email", "profile", "offline_access"],
        }),
      ),
    );
    await setUpOktaSignIn(core, {
      ...baseInput,
      existingClient: makeClient(),
      keySet: { kind: "existing", setId: "new-set" },
      onKeySetCreated,
    });
    expect(api.createSet).toHaveBeenCalledTimes(1);
    expect(api.attachKeySet).toHaveBeenLastCalledWith(core, {
      attachKeySetForm: { id: "client", jsonWebKeySetId: "new-set" },
    });
  });

  it("repairs the assertion audience on a client already using private_key_jwt", async () => {
    const client = makeClient({
      jsonWebKeySetId: "set",
      tokenEndpointAuthMethod: "private_key_jwt",
      scope: ["openid", "email", "profile", "offline_access"],
    });
    expect(isSignInClientReady(client)).toBe(false);
    api.updateClient.mockReturnValue(
      ok({ ...client, tokenEndpointAuthAudienceFormat: "token_endpoint" }),
    );

    await setUpOktaSignIn(core, {
      ...baseInput,
      existingClient: client,
      keySet: undefined,
    });

    expect(api.updateClient).toHaveBeenCalledWith(core, {
      updateRemoteSessionClientForm: expect.objectContaining({
        id: "client",
        tokenEndpointAuthAudienceFormat: "token_endpoint",
      }),
    });
  });

  it("leaves a ready client alone", async () => {
    const client = makeClient({
      jsonWebKeySetId: "set",
      tokenEndpointAuthMethod: "private_key_jwt",
      tokenEndpointAuthAudienceFormat: "token_endpoint",
      scope: ["openid", "email", "profile", "offline_access"],
    });
    expect(isSignInClientReady(client)).toBe(true);

    await setUpOktaSignIn(core, {
      ...baseInput,
      existingClient: client,
      keySet: undefined,
    });

    expect(api.updateClient).not.toHaveBeenCalled();
    expect(api.createClient).not.toHaveBeenCalled();
    expect(api.updateIssuer).not.toHaveBeenCalled();
  });
});
