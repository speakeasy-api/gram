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
  parseScopes,
  type Context,
  type DiscoveredOAuth,
  type Input,
} from "./machine-types";
import {
  nextEnvironmentName,
  type AddExternalOAuthInput,
  type AddOAuthProxyInput,
  type CreateEnvironmentInput,
  type CreateEnvironmentOutput,
  type DeleteEnvironmentInput,
  type RegisterClientInput,
  type RegisterClientOutput,
} from "./services";

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

const VALID_PROXY_METADATA = {
  authorization_endpoint: "https://example.com/auth",
  token_endpoint: "https://example.com/token",
  registration_endpoint: "https://example.com/register",
  scopes_supported: ["read", "write"],
};

const VALID_EXTERNAL_METADATA_JSON = JSON.stringify(VALID_PROXY_METADATA);

const DISCOVERED_2_1: DiscoveredOAuth = {
  slug: "discovered-slug",
  name: "Discovered",
  version: "2.1",
  metadata: VALID_PROXY_METADATA,
};

const baseInput: Input = {
  discovered: null,
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
    createEnvironment: fromPromise<
      CreateEnvironmentOutput,
      CreateEnvironmentInput
    >(async () => ({ envSlug: "env-new" })),
    addOAuthProxy: fromPromise<void, AddOAuthProxyInput>(async () => {}),
    deleteEnvironment: fromPromise<void, DeleteEnvironmentInput>(
      async () => {},
    ),
    registerClient: fromPromise<RegisterClientOutput, RegisterClientInput>(
      async () => ({
        clientId: "auto-cid",
        clientSecret: "auto-secret",
        tokenAuthMethod: "client_secret_basic",
      }),
    ),
  };
}

function servicesWithRegisterFailure() {
  return {
    ...happyServices(),
    registerClient: fromPromise<RegisterClientOutput, RegisterClientInput>(
      async () => {
        throw new Error("DCR boom");
      },
    ),
  };
}

function servicesWithProxyFailure() {
  return {
    ...happyServices(),
    addOAuthProxy: fromPromise<void, AddOAuthProxyInput>(async () => {
      throw new Error("proxy boom");
    }),
  };
}

function servicesWithEnvFailure() {
  return {
    ...happyServices(),
    createEnvironment: fromPromise<
      CreateEnvironmentOutput,
      CreateEnvironmentInput
    >(async () => {
      throw new Error("env boom");
    }),
  };
}

function servicesWithDoubleFailure() {
  return {
    ...happyServices(),
    addOAuthProxy: fromPromise<void, AddOAuthProxyInput>(async () => {
      throw new Error("proxy boom");
    }),
    deleteEnvironment: fromPromise<void, DeleteEnvironmentInput>(async () => {
      throw new Error("rollback boom");
    }),
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

describe("nextEnvironmentName", () => {
  it("returns base name when no collision", () => {
    expect(nextEnvironmentName("MyTool", [])).toBe("MyTool OAuth");
  });

  it("appends 1 on first collision", () => {
    expect(nextEnvironmentName("MyTool", ["MyTool OAuth"])).toBe(
      "MyTool OAuth 1",
    );
  });

  it("walks suffixes until finding a free slot", () => {
    expect(
      nextEnvironmentName("MyTool", [
        "MyTool OAuth",
        "MyTool OAuth 1",
        "MyTool OAuth 2",
      ]),
    ).toBe("MyTool OAuth 3");
  });
});

// ---------------------------------------------------------------------------
// Guards (pure)
// ---------------------------------------------------------------------------

function withExternal(over: Partial<Context["external"]> = {}): Context {
  return {
    discovered: null,
    external: {
      slug: "ok",
      metadataJson: VALID_EXTERNAL_METADATA_JSON,
      jsonError: null,
      prefilled: false,
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
      prefilled: false,
    },
    envSlug: null,
    error: null,
    autoRegistering: false,
    result: null,
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
      prefilled: false,
      ...over,
    },
  };
}

describe("checkExternal", () => {
  it("accepts valid input", () => {
    expect(checkExternal(withExternal())).toEqual({ ok: true });
  });

  it("rejects empty slug", () => {
    expect(checkExternal(withExternal({ slug: "  " }))).toMatchObject({
      ok: false,
      reason: expect.stringContaining("slug"),
    });
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
    expect(validExternal(withExternal({ slug: "" }))).toBe(false);
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
    expect(ctx.external.slug).toBe("");
  });
});

describe("oauthWizardMachine — path selection", () => {
  it("SELECT_PROXY moves to proxy.metadata", () => {
    const actor = makeActor(baseInput);
    actor.start();
    actor.send({ type: "SELECT_PROXY" });
    expect(actor.getSnapshot().matches({ proxy: "metadata" })).toBe(true);
  });

  it("SELECT_PROXY pre-fills proxy form from discovered metadata", () => {
    const actor = makeActor({ ...baseInput, discovered: DISCOVERED_2_1 });
    actor.start();
    actor.send({ type: "SELECT_PROXY" });
    const { proxy } = actor.getSnapshot().context;
    expect(proxy.slug).toBe("discovered-slug");
    expect(proxy.authorizationEndpoint).toBe(
      VALID_PROXY_METADATA.authorization_endpoint,
    );
    expect(proxy.scopes).toBe("read, write");
    expect(proxy.prefilled).toBe(true);
  });

  it("SELECT_EXTERNAL moves to external.editing", () => {
    const actor = makeActor(baseInput);
    actor.start();
    actor.send({ type: "SELECT_EXTERNAL" });
    expect(actor.getSnapshot().matches({ external: "editing" })).toBe(true);
  });

  it("SELECT_EXTERNAL prefills metadata JSON when discovered version is 2.1", () => {
    const actor = makeActor({ ...baseInput, discovered: DISCOVERED_2_1 });
    actor.start();
    actor.send({ type: "SELECT_EXTERNAL" });
    const { external } = actor.getSnapshot().context;
    expect(external.slug).toBe("discovered-slug");
    expect(external.prefilled).toBe(true);
    expect(JSON.parse(external.metadataJson)).toEqual(VALID_PROXY_METADATA);
  });

  it("SELECT_EXTERNAL does not prefill when discovered version is not 2.1", () => {
    const actor = makeActor({
      ...baseInput,
      discovered: { ...DISCOVERED_2_1, version: "1.0" },
    });
    actor.start();
    actor.send({ type: "SELECT_EXTERNAL" });
    const { external } = actor.getSnapshot().context;
    expect(external.slug).toBe("");
    expect(external.prefilled).toBe(false);
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
    expect(snap.context.envSlug).toBe("env-new");
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

describe("oauthWizardMachine — rollback on proxy failure", () => {
  it("creatingProxy failure → rollingBackEnv → credentials with error; envSlug cleared", async () => {
    const actor = makeActor(baseInput, servicesWithProxyFailure());
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
      {
        timeout: 1000,
      },
    );

    const snap = actor.getSnapshot();
    expect(snap.context.error).toContain("proxy boom");
    expect(snap.context.envSlug).toBeNull();
  });

  it("rollback failure routes to fatalError (terminal) with compound error", async () => {
    const actor = makeActor(baseInput, servicesWithDoubleFailure());
    actor.start();
    actor.send({ type: "SELECT_PROXY" });
    fillProxyForm(actor);
    actor.send({ type: "NEXT" });
    actor.send({ type: "FIELD_PROXY", key: "clientId", value: "cid" });
    actor.send({ type: "FIELD_PROXY", key: "clientSecret", value: "csec" });
    actor.send({ type: "SUBMIT" });

    await waitFor(actor, (s) => s.matches({ proxy: "fatalError" }), {
      timeout: 1000,
    });

    const snap = actor.getSnapshot();
    expect(snap.context.error).toContain("proxy boom");
    expect(snap.context.error).toContain("rollback boom");
    expect(snap.context.error).toContain("manually");
    // fatalError is terminal — sending more events doesn't move us
    actor.send({ type: "BACK" });
    expect(actor.getSnapshot().matches({ proxy: "fatalError" })).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// Machine — external happy path and APPLY_DISCOVERED
// ---------------------------------------------------------------------------

describe("oauthWizardMachine — external happy path", () => {
  it("walks editing → submitting → result.success", async () => {
    const actor = makeActor(baseInput);
    actor.start();
    actor.send({ type: "SELECT_EXTERNAL" });
    actor.send({ type: "FIELD_EXTERNAL", key: "slug", value: "ext-slug" });
    actor.send({
      type: "FIELD_EXTERNAL",
      key: "metadataJson",
      value: VALID_EXTERNAL_METADATA_JSON,
    });
    actor.send({ type: "SUBMIT" });

    await waitFor(actor, (s) => s.matches({ result: "success" }), {
      timeout: 1000,
    });

    expect(actor.getSnapshot().context.result?.success).toBe(true);
  });

  it("SUBMIT with invalid JSON stays in editing with jsonError set", () => {
    const actor = makeActor(baseInput);
    actor.start();
    actor.send({ type: "SELECT_EXTERNAL" });
    actor.send({ type: "FIELD_EXTERNAL", key: "slug", value: "ext-slug" });
    actor.send({
      type: "FIELD_EXTERNAL",
      key: "metadataJson",
      value: "{not-json",
    });
    actor.send({ type: "SUBMIT" });
    const snap = actor.getSnapshot();
    expect(snap.matches({ external: "editing" })).toBe(true);
    expect(snap.context.external.jsonError).toBe("Invalid JSON format");
  });

  it("APPLY_DISCOVERED applies discovery to external form", () => {
    const actor = makeActor({ ...baseInput, discovered: DISCOVERED_2_1 });
    actor.start();
    actor.send({ type: "SELECT_EXTERNAL" });
    // SELECT_EXTERNAL already applied discovery; clear and re-apply
    actor.send({ type: "FIELD_EXTERNAL", key: "slug", value: "" });
    actor.send({ type: "APPLY_DISCOVERED" });
    expect(actor.getSnapshot().context.external.slug).toBe("discovered-slug");
  });
});

// ---------------------------------------------------------------------------
// Machine — manual proxy path always prompts for credentials
// ---------------------------------------------------------------------------

describe("oauthWizardMachine — manual proxy path skips auto-register chooser", () => {
  const inputWithDiscovered: Input = {
    ...baseInput,
    discovered: DISCOVERED_2_1,
  };

  it("NEXT from metadata routes directly to credentials even when registration_endpoint is discovered", () => {
    const actor = makeActor(inputWithDiscovered);
    actor.start();
    actor.send({ type: "SELECT_PROXY" });
    fillProxyForm(actor);
    actor.send({ type: "NEXT" });
    const snap = actor.getSnapshot();
    expect(snap.matches({ proxy: "credentials" })).toBe(true);
    expect(snap.context.autoRegistering).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// Machine — zero-click auto-configure from path selection
// ---------------------------------------------------------------------------

describe("oauthWizardMachine — auto-configure from path selection", () => {
  const inputWithDiscovered: Input = {
    ...baseInput,
    discovered: DISCOVERED_2_1,
  };

  it("SELECT_PROXY_AUTO transitions to proxy.registering with prefilled proxy and autoRegistering=true", () => {
    const actor = makeActor(inputWithDiscovered);
    actor.start();
    actor.send({ type: "SELECT_PROXY_AUTO" });
    const snap = actor.getSnapshot();
    expect(snap.matches({ proxy: "registering" })).toBe(true);
    expect(snap.context.autoRegistering).toBe(true);
    expect(snap.context.proxy.slug).toBe("discovered-slug");
    expect(snap.context.proxy.authorizationEndpoint).toBe(
      VALID_PROXY_METADATA.authorization_endpoint,
    );
    expect(snap.context.proxy.tokenEndpoint).toBe(
      VALID_PROXY_METADATA.token_endpoint,
    );
    expect(snap.context.proxy.scopes).toBe("read, write");
    expect(snap.context.proxy.prefilled).toBe(true);
  });

  it("walks pathSelection → registering → submitting → result.success without touching metadata or autoRegisterChoice", async () => {
    const actor = makeActor(inputWithDiscovered);
    actor.start();
    actor.send({ type: "SELECT_PROXY_AUTO" });

    await waitFor(actor, (s) => s.matches({ result: "success" }), {
      timeout: 1000,
    });

    const snap = actor.getSnapshot();
    expect(snap.context.proxy.clientId).toBe("auto-cid");
    expect(snap.context.proxy.clientSecret).toBe("auto-secret");
    expect(snap.context.envSlug).toBe("env-new");
    expect(snap.context.result?.success).toBe(true);
  });

  it("registerClient failure routes to autoRegisterFailed", async () => {
    const actor = makeActor(inputWithDiscovered, servicesWithRegisterFailure());
    actor.start();
    actor.send({ type: "SELECT_PROXY_AUTO" });

    await waitFor(actor, (s) => s.matches({ proxy: "autoRegisterFailed" }), {
      timeout: 1000,
    });

    expect(actor.getSnapshot().context.error).toContain("DCR boom");
  });

  it("addOAuthProxy failure rolls back env and lands on autoRegisterFailed (does not drop into manual creds form)", async () => {
    const actor = makeActor(inputWithDiscovered, servicesWithProxyFailure());
    actor.start();
    actor.send({ type: "SELECT_PROXY_AUTO" });

    await waitFor(actor, (s) => s.matches({ proxy: "autoRegisterFailed" }), {
      timeout: 1000,
    });

    const snap = actor.getSnapshot();
    expect(snap.context.error).toContain("proxy boom");
    expect(snap.context.envSlug).toBeNull();
  });

  it("createEnvironment failure lands on autoRegisterFailed (does not drop into manual creds form)", async () => {
    const actor = makeActor(inputWithDiscovered, servicesWithEnvFailure());
    actor.start();
    actor.send({ type: "SELECT_PROXY_AUTO" });

    await waitFor(actor, (s) => s.matches({ proxy: "autoRegisterFailed" }), {
      timeout: 1000,
    });

    const snap = actor.getSnapshot();
    expect(snap.context.error).toContain("env boom");
  });

  it("SELECT_PROXY_AUTO is a no-op when no discovered metadata (UI hides the card in this case)", () => {
    const actor = makeActor(baseInput);
    actor.start();
    actor.send({ type: "SELECT_PROXY_AUTO" });
    expect(actor.getSnapshot().matches("pathSelection")).toBe(true);
  });

  it("SELECT_PROXY_AUTO is a no-op when registration_endpoint missing (UI hides the card in this case)", () => {
    const { registration_endpoint: _omit, ...metadataWithoutRegistration } =
      VALID_PROXY_METADATA;
    const actor = makeActor({
      ...baseInput,
      discovered: {
        ...DISCOVERED_2_1,
        metadata: metadataWithoutRegistration,
      },
    });
    actor.start();
    actor.send({ type: "SELECT_PROXY_AUTO" });
    expect(actor.getSnapshot().matches("pathSelection")).toBe(true);
  });

  it("SELECT_PROXY_AUTO proceeds to proxy.registering even when scopes_supported missing (scopes are optional)", () => {
    const { scopes_supported: _omit, ...metadataWithoutScopes } =
      VALID_PROXY_METADATA;
    const actor = makeActor({
      ...baseInput,
      discovered: {
        ...DISCOVERED_2_1,
        metadata: metadataWithoutScopes,
      },
    });
    actor.start();
    actor.send({ type: "SELECT_PROXY_AUTO" });
    const snap = actor.getSnapshot();
    expect(snap.matches({ proxy: "registering" })).toBe(true);
    expect(snap.context.autoRegistering).toBe(true);
    expect(snap.context.proxy.scopes).toBe("");
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
