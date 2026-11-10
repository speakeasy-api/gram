// @vitest-environment node
import { describe, expect, it } from "vitest";
import { createActor, fromPromise, waitFor } from "xstate";

import {
  checkCreds,
  checkExternal,
  checkProxyMeta,
  validCreds,
  validExternal,
  validProxyMeta,
} from "./guards";
import { oauthWizardMachine } from "./machine";
import {
  authServerOrigin,
  parseScopes,
  type Context,
  type Input,
} from "./machine-types";
import {
  type AddExternalOAuthInput,
  type DiscoverExternalOAuthInput,
  type DiscoverExternalOAuthOutput,
  type ProvisionUserSessionInput,
} from "./services";

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

const VALID_PROXY_METADATA = {
  issuer: "https://example.com",
  authorization_endpoint: "https://example.com/auth",
  token_endpoint: "https://example.com/token",
  registration_endpoint: "https://example.com/register",
  scopes_supported: ["read", "write"],
};

const VALID_EXTERNAL_METADATA_JSON = JSON.stringify(VALID_PROXY_METADATA);

const baseInput: Input = {
  toolsetId: "toolset-1",
  toolsetSlug: "ts",
  toolsetName: "Toolset",
  activeOrganizationId: "org-1",
};

// ---------------------------------------------------------------------------
// Mock service factories
// ---------------------------------------------------------------------------

function happyServices() {
  return {
    addExternalOAuth: fromPromise<void, AddExternalOAuthInput>(async () => {}),
    discoverExternalOAuth: fromPromise<
      DiscoverExternalOAuthOutput,
      DiscoverExternalOAuthInput
    >(async ({ input }) => ({
      issuer: input.issuer,
      authorizationEndpoint: `${input.issuer}/authorize`,
      tokenEndpoint: `${input.issuer}/token`,
      authorizationResponseIssParameterSupported: true,
      backchannelLogoutSupported: false,
      clientIdMetadataDocumentSupported: false,
      discoveryWarnings: [],
      oidc: false,
      passthrough: true,
    })),
    provisionUserSession: fromPromise<void, ProvisionUserSessionInput>(
      async () => {},
    ),
  };
}

function servicesWithProvisionFailure() {
  return {
    ...happyServices(),
    provisionUserSession: fromPromise<void, ProvisionUserSessionInput>(
      async () => {
        throw new Error("provision boom");
      },
    ),
  };
}

function makeActor(
  input: Input,
  services: ReturnType<typeof happyServices> = happyServices(),
) {
  return createActor(oauthWizardMachine.provide({ actors: services }), {
    input,
  });
}

function fillProxyForm(actor: ReturnType<typeof makeActor>) {
  const fields: Array<[string, string]> = [
    ["slug", "my-proxy"],
    ["authorizationEndpoint", "https://example.com/auth"],
    ["tokenEndpoint", "https://example.com/token"],
    ["scopes", "read, write"],
  ];
  for (const [key, value] of fields) {
    actor.send({
      type: "FIELD_PROXY",
      key: key as Parameters<typeof actor.send>[0] extends infer E
        ? E extends { type: "FIELD_PROXY"; key: infer K }
          ? K
          : never
        : never,
      value,
    });
  }
}

// ---------------------------------------------------------------------------
// parseScopes / audienceDirty / nextEnvironmentName
// ---------------------------------------------------------------------------

describe("parseScopes", () => {
  it("splits comma-separated scopes and trims whitespace", () => {
    expect(parseScopes("read, write,  admin ")).toEqual([
      "read",
      "write",
      "admin",
    ]);
  });

  it("filters out empty entries", () => {
    expect(parseScopes(", , read,  ,")).toEqual(["read"]);
  });

  it("returns empty array for empty input", () => {
    expect(parseScopes("")).toEqual([]);
  });
});

// ---------------------------------------------------------------------------
// Guards (pure)
// ---------------------------------------------------------------------------

function withExternal(over: Partial<Context["external"]> = {}): Context {
  return {
    external: {
      issuerUrl: "https://example.com",
      metadataJson: VALID_EXTERNAL_METADATA_JSON,
      verifiedMetadata: null,
      jsonError: null,
      ...over,
    },
    proxy: {
      slug: "",
      authorizationEndpoint: "",
      tokenEndpoint: "",
      scopes: "",
      audience: "",
      tokenAuthMethod: "client_secret_post",
      environmentSlug: "",
      clientId: "",
      clientSecret: "",
    },
    envSlug: null,
    error: null,
    result: null,
    toolsetId: "",
    toolsetSlug: "",
    toolsetName: "",
    activeOrganizationId: "",
  };
}

function withProxy(over: Partial<Context["proxy"]> = {}): Context {
  return {
    ...withExternal(),
    proxy: {
      slug: "ok",
      authorizationEndpoint: "https://e.example/auth",
      tokenEndpoint: "https://e.example/token",
      scopes: "read",
      audience: "",
      tokenAuthMethod: "client_secret_post",
      environmentSlug: "",
      clientId: "id",
      clientSecret: "secret",
      ...over,
    },
  };
}

describe("checkExternal", () => {
  it("accepts valid input", () => {
    expect(checkExternal(withExternal())).toEqual({ ok: true });
  });

  it("does not require a dashboard-entered slug", () => {
    expect(checkExternal(withExternal())).toEqual({ ok: true });
  });

  it("rejects malformed JSON", () => {
    expect(
      checkExternal(withExternal({ metadataJson: "{not json" })),
    ).toMatchObject({ ok: false, reason: "Invalid JSON format" });
  });

  it("rejects missing required endpoints", () => {
    const partial = JSON.stringify({
      authorization_endpoint: "https://e.example/auth",
      token_endpoint: "https://e.example/token",
    });
    expect(
      checkExternal(withExternal({ metadataJson: partial })),
    ).toMatchObject({
      ok: false,
      reason: expect.stringContaining("registration_endpoint"),
    });
  });

  it("rejects malformed endpoint URLs", () => {
    const bad = JSON.stringify({
      authorization_endpoint: "not-a-url",
      token_endpoint: "https://e.example/token",
      registration_endpoint: "https://e.example/register",
    });
    expect(checkExternal(withExternal({ metadataJson: bad }))).toMatchObject({
      ok: false,
      reason: expect.stringContaining("authorization_endpoint"),
    });
  });

  it("rejects array as metadata", () => {
    expect(
      checkExternal(withExternal({ metadataJson: "[1,2,3]" })),
    ).toMatchObject({ ok: false, reason: "Metadata must be a JSON object" });
  });

  it("rejects primitive JSON as metadata", () => {
    expect(checkExternal(withExternal({ metadataJson: "42" }))).toMatchObject({
      ok: false,
      reason: "Metadata must be a JSON object",
    });
  });
});

describe("checkProxyMeta", () => {
  it("accepts valid input", () => {
    expect(checkProxyMeta(withProxy())).toEqual({ ok: true });
  });

  it("rejects empty slug", () => {
    expect(checkProxyMeta(withProxy({ slug: "" }))).toMatchObject({
      ok: false,
    });
  });

  it("rejects empty authorization endpoint", () => {
    expect(
      checkProxyMeta(withProxy({ authorizationEndpoint: "" })),
    ).toMatchObject({ ok: false });
  });

  it("rejects empty token endpoint", () => {
    expect(checkProxyMeta(withProxy({ tokenEndpoint: "" }))).toMatchObject({
      ok: false,
    });
  });

  it("accepts empty scopes (server allows zero-scope proxies)", () => {
    expect(checkProxyMeta(withProxy({ scopes: "" }))).toEqual({ ok: true });
  });

  it("accepts scopes containing only whitespace", () => {
    expect(checkProxyMeta(withProxy({ scopes: " , , " }))).toEqual({
      ok: true,
    });
  });
});

describe("checkCreds", () => {
  it("accepts both filled", () => {
    expect(checkCreds(withProxy())).toEqual({ ok: true });
  });

  it("rejects missing client id", () => {
    expect(checkCreds(withProxy({ clientId: " " }))).toMatchObject({
      ok: false,
    });
  });

  it("rejects missing client secret", () => {
    expect(checkCreds(withProxy({ clientSecret: "" }))).toMatchObject({
      ok: false,
    });
  });
});

describe("guard boolean wrappers", () => {
  it("validExternal mirrors checkExternal", () => {
    expect(validExternal(withExternal())).toBe(true);
    expect(validExternal(withExternal({ metadataJson: "{not json" }))).toBe(
      false,
    );
  });

  it("validProxyMeta mirrors checkProxyMeta", () => {
    expect(validProxyMeta(withProxy())).toBe(true);
    expect(validProxyMeta(withProxy({ slug: "" }))).toBe(false);
  });

  it("validCreds mirrors checkCreds", () => {
    expect(validCreds(withProxy())).toBe(true);
    expect(validCreds(withProxy({ clientId: "" }))).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// Machine — initial state and path selection
// ---------------------------------------------------------------------------

describe("oauthWizardMachine — initial state", () => {
  it("starts in pathSelection", () => {
    const actor = makeActor(baseInput);
    actor.start();
    expect(actor.getSnapshot().matches("pathSelection")).toBe(true);
  });

  it("populates context from input", () => {
    const actor = makeActor({ ...baseInput, toolsetName: "MyTool" });
    actor.start();
    const ctx = actor.getSnapshot().context;
    expect(ctx.toolsetName).toBe("MyTool");
    expect(ctx.proxy.slug).toBe("");
    expect(ctx.external).not.toHaveProperty("slug");
  });
});

describe("oauthWizardMachine — path selection", () => {
  it("SELECT_PROXY moves to proxy.metadata", () => {
    const actor = makeActor(baseInput);
    actor.start();
    actor.send({ type: "SELECT_PROXY" });
    expect(actor.getSnapshot().matches({ proxy: "metadata" })).toBe(true);
  });

  it("SELECT_EXTERNAL moves to external.source", () => {
    const actor = makeActor(baseInput);
    actor.start();
    actor.send({ type: "SELECT_EXTERNAL" });
    expect(actor.getSnapshot().matches({ external: "source" })).toBe(true);
  });

});

// ---------------------------------------------------------------------------
// Machine — proxy metadata validation
// ---------------------------------------------------------------------------

describe("oauthWizardMachine — proxy.metadata validation", () => {
  it("NEXT with invalid form stays in metadata and sets error", () => {
    const actor = makeActor(baseInput);
    actor.start();
    actor.send({ type: "SELECT_PROXY" });
    actor.send({ type: "NEXT" });
    const snap = actor.getSnapshot();
    expect(snap.matches({ proxy: "metadata" })).toBe(true);
    expect(snap.context.error).toBeTruthy();
  });

  it("NEXT with valid form moves to credentials and clears error", () => {
    const actor = makeActor(baseInput);
    actor.start();
    actor.send({ type: "SELECT_PROXY" });
    fillProxyForm(actor);
    actor.send({ type: "NEXT" });
    const snap = actor.getSnapshot();
    expect(snap.matches({ proxy: "credentials" })).toBe(true);
    expect(snap.context.error).toBeNull();
  });

  it("BACK from metadata returns to pathSelection", () => {
    const actor = makeActor(baseInput);
    actor.start();
    actor.send({ type: "SELECT_PROXY" });
    actor.send({ type: "BACK" });
    expect(actor.getSnapshot().matches("pathSelection")).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// Machine — happy proxy create path
// ---------------------------------------------------------------------------

describe("oauthWizardMachine — happy proxy create", () => {
  it("walks metadata → credentials → creatingEnvironment → creatingProxy → result.success", async () => {
    const actor = makeActor(baseInput);
    actor.start();

    actor.send({ type: "SELECT_PROXY" });
    fillProxyForm(actor);
    actor.send({ type: "NEXT" });

    actor.send({ type: "FIELD_PROXY", key: "clientId", value: "cid" });
    actor.send({ type: "FIELD_PROXY", key: "clientSecret", value: "csec" });
    actor.send({ type: "SUBMIT" });

    await waitFor(actor, (s) => s.matches({ result: "success" }), {
      timeout: 1000,
    });

    const snap = actor.getSnapshot();
    expect(snap.context.result?.success).toBe(true);
  });

  it("SUBMIT with invalid creds stays in credentials with error", () => {
    const actor = makeActor(baseInput);
    actor.start();
    actor.send({ type: "SELECT_PROXY" });
    fillProxyForm(actor);
    actor.send({ type: "NEXT" });
    actor.send({ type: "SUBMIT" });
    const snap = actor.getSnapshot();
    expect(snap.matches({ proxy: "credentials" })).toBe(true);
    expect(snap.context.error).toBeTruthy();
  });
});

// ---------------------------------------------------------------------------
// Machine — partial-failure rollback
// ---------------------------------------------------------------------------

describe("oauthWizardMachine — provisioning failure", () => {
  it("provisionUserSession failure returns to credentials with error", async () => {
    const actor = makeActor(baseInput, servicesWithProvisionFailure());
    actor.start();
    actor.send({ type: "SELECT_PROXY" });
    fillProxyForm(actor);
    actor.send({ type: "NEXT" });
    actor.send({ type: "FIELD_PROXY", key: "clientId", value: "cid" });
    actor.send({ type: "FIELD_PROXY", key: "clientSecret", value: "csec" });
    actor.send({ type: "SUBMIT" });

    await waitFor(
      actor,
      (s) => s.matches({ proxy: "credentials" }) && !!s.context.error,
      { timeout: 1000 },
    );

    expect(actor.getSnapshot().context.error).toContain("provision boom");
  });
});

// ---------------------------------------------------------------------------
// Machine — external happy path
// ---------------------------------------------------------------------------

describe("oauthWizardMachine - external happy path", () => {
  it("walks source to provider issuer to verifying to review to submitting", async () => {
    let submitted: AddExternalOAuthInput | undefined;
    const services = {
      ...happyServices(),
      addExternalOAuth: fromPromise<void, AddExternalOAuthInput>(
        async ({ input }) => {
          submitted = input;
        },
      ),
    };
    const actor = makeActor(baseInput, services);
    actor.start();
    actor.send({ type: "SELECT_EXTERNAL" });
    expect(actor.getSnapshot().matches({ external: "source" })).toBe(true);
    actor.send({ type: "SELECT_PROVIDER_ISSUER" });
    actor.send({
      type: "FIELD_EXTERNAL",
      key: "issuerUrl",
      value: "https://example.com",
    });
    actor.send({ type: "NEXT" });
    expect(actor.getSnapshot().matches({ external: "verifying" })).toBe(true);
    await waitFor(actor, (s) => s.matches({ external: "review" }));
    actor.send({ type: "SUBMIT" });
    await waitFor(actor, (s) => s.matches({ result: "success" }));
    expect(submitted).toEqual({
      toolsetSlug: "ts",
      authorizationServerIssuer: "https://example.com",
    });
  });

  it("walks source to Speakeasy-hosted to submitting without a slug", async () => {
    let submitted: AddExternalOAuthInput | undefined;
    const services = {
      ...happyServices(),
      addExternalOAuth: fromPromise<void, AddExternalOAuthInput>(
        async ({ input }) => {
          submitted = input;
        },
      ),
    };
    const actor = makeActor(baseInput, services);
    actor.start();
    actor.send({ type: "SELECT_EXTERNAL" });
    actor.send({ type: "SELECT_GRAM_HOSTED" });
    actor.send({
      type: "FIELD_EXTERNAL",
      key: "metadataJson",
      value: VALID_EXTERNAL_METADATA_JSON,
    });
    actor.send({ type: "SUBMIT" });
    await waitFor(actor, (s) => s.matches({ result: "success" }));
    expect(submitted).toEqual({
      toolsetSlug: "ts",
      metadata: VALID_PROXY_METADATA,
    });
  });

  it("invalidates verified discovery when the issuer changes", async () => {
    const actor = makeActor(baseInput);
    actor.start();
    actor.send({ type: "SELECT_EXTERNAL" });
    actor.send({ type: "SELECT_PROVIDER_ISSUER" });
    actor.send({
      type: "FIELD_EXTERNAL",
      key: "issuerUrl",
      value: "https://example.com",
    });
    actor.send({ type: "NEXT" });
    await waitFor(actor, (s) => s.matches({ external: "review" }));
    actor.send({
      type: "FIELD_EXTERNAL",
      key: "issuerUrl",
      value: "https://other.example.com",
    });
    expect(actor.getSnapshot().matches({ external: "providerIssuer" })).toBe(
      true,
    );
    expect(actor.getSnapshot().context.external.verifiedMetadata).toBeNull();
    actor.send({ type: "SUBMIT" });
    expect(actor.getSnapshot().matches({ external: "providerIssuer" })).toBe(
      true,
    );
  });

  it("failed discovery cannot submit and clears when leaving", async () => {
    const services = {
      ...happyServices(),
      discoverExternalOAuth: fromPromise<
        DiscoverExternalOAuthOutput,
        DiscoverExternalOAuthInput
      >(async () => {
        throw new Error("Discovery failed");
      }),
    };
    const actor = makeActor(baseInput, services);
    actor.start();
    actor.send({ type: "SELECT_EXTERNAL" });
    actor.send({ type: "SELECT_PROVIDER_ISSUER" });
    actor.send({
      type: "FIELD_EXTERNAL",
      key: "issuerUrl",
      value: "https://example.com",
    });
    actor.send({ type: "NEXT" });
    await waitFor(
      actor,
      (s) => s.matches({ external: "providerIssuer" }) && !!s.context.error,
    );
    actor.send({ type: "SUBMIT" });
    expect(actor.getSnapshot().matches({ external: "providerIssuer" })).toBe(
      true,
    );

    actor.send({ type: "BACK" });
    expect(actor.getSnapshot().matches({ external: "source" })).toBe(true);
    expect(actor.getSnapshot().context.error).toBeNull();
  });

  it("keeps invalid manual JSON in the Speakeasy-hosted editor", () => {
    const actor = makeActor(baseInput);
    actor.start();
    actor.send({ type: "SELECT_EXTERNAL" });
    actor.send({ type: "SELECT_GRAM_HOSTED" });
    actor.send({
      type: "FIELD_EXTERNAL",
      key: "metadataJson",
      value: "{not-json",
    });
    actor.send({ type: "SUBMIT" });
    expect(actor.getSnapshot().matches({ external: "gramHosted" })).toBe(true);
    expect(actor.getSnapshot().context.external.jsonError).toBe(
      "Invalid JSON format",
    );
  });
});

describe("authServerOrigin", () => {
  it("returns the origin of the first parseable URL", () => {
    expect(authServerOrigin("https://www.make.com/oauth/v2/authorize")).toBe(
      "https://www.make.com",
    );
    expect(authServerOrigin("", "not a url", "https://x.test/token")).toBe(
      "https://x.test",
    );
  });

  it("returns empty string when no candidate parses", () => {
    expect(authServerOrigin("", "  ", "relative/path")).toBe("");
  });
});

// ---------------------------------------------------------------------------
// Machine — placeholder actor sanity
// ---------------------------------------------------------------------------

describe("oauthWizardMachine — placeholder actors", () => {
  it("invoking a state without providing actors throws a clear error", async () => {
    const actor = createActor(oauthWizardMachine, { input: baseInput });
    actor.start();
    actor.send({ type: "SELECT_PROXY" });
    fillProxyForm(actor);
    actor.send({ type: "NEXT" });
    actor.send({ type: "FIELD_PROXY", key: "clientId", value: "cid" });
    actor.send({ type: "FIELD_PROXY", key: "clientSecret", value: "csec" });
    actor.send({ type: "SUBMIT" });

    // Placeholder createEnvironment throws → onError sends us back to credentials.
    await waitFor(
      actor,
      (s) => s.matches({ proxy: "credentials" }) && !!s.context.error,
      { timeout: 1000 },
    );
    expect(actor.getSnapshot().context.error).toContain("not provided");
  });
});
