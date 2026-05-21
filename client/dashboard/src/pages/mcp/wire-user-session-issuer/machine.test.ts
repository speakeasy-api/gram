import type {
  OAuthProxyProvider,
  RemoteSessionClient,
  RemoteSessionIssuer,
  UserSessionIssuer,
} from "@gram/client/models/components";
import { describe, expect, it } from "vitest";
import { createActor, fromPromise, waitFor } from "xstate";

import type { MigrationDefaults } from "./defaults";
import {
  selectCurrentStep,
  selectErrorStep,
  selectIsComplete,
  selectSteps,
  wireUserSessionIssuerMachine,
} from "./machine";
import type {
  CreateRemoteSessionClientInput,
  CreateRemoteSessionIssuerInput,
  CreateUserSessionIssuerInput,
  LinkToolsetUserSessionIssuerInput,
  MigrationInput,
  ResolveRemoteSessionClientInput,
  ResolveRemoteSessionIssuerInput,
  ResolveUserSessionIssuerInput,
} from "./machine-types";

const proxyProvider: OAuthProxyProvider = {
  id: "proxy-provider-1",
  slug: "github",
  providerType: "custom",
  authorizationEndpoint: "https://idp.example.com/oauth/authorize",
  tokenEndpoint: "https://idp.example.com/oauth/token",
  scopesSupported: ["repo"],
  grantTypesSupported: ["authorization_code", "refresh_token"],
  tokenEndpointAuthMethodsSupported: ["client_secret_basic"],
  createdAt: new Date(0),
  updatedAt: new Date(0),
};

const defaults: MigrationDefaults = {
  proxyProvider,
  userSessionIssuerSlug: "github-a1b2c3d4",
  remoteSessionIssuerSlug: "github-a1b2c3d4",
  issuerOriginGuess: "https://idp.example.com",
  sessionDurationHours: 24 * 14,
};

const baseInput: MigrationInput = {
  defaults,
  paradigm: "custom",
  toolsetSlug: "github",
};

function userSessionIssuer(id = "usi-1"): UserSessionIssuer {
  return {
    id,
    projectId: "project-1",
    slug: defaults.userSessionIssuerSlug,
    authnChallengeMode: "interactive",
    sessionDurationHours: defaults.sessionDurationHours,
    createdAt: new Date(0),
    updatedAt: new Date(0),
  };
}

function remoteSessionIssuer(id = "rsi-1"): RemoteSessionIssuer {
  return {
    id,
    projectId: "project-1",
    slug: defaults.remoteSessionIssuerSlug,
    issuer: "https://idp.example.com",
    oidc: false,
    passthrough: false,
    authorizationEndpoint: "https://idp.example.com/oauth/authorize",
    tokenEndpoint: "https://idp.example.com/oauth/token",
    registrationEndpoint: "https://idp.example.com/oauth/register",
    scopesSupported: ["repo"],
    grantTypesSupported: ["authorization_code", "refresh_token"],
    responseTypesSupported: ["code"],
    tokenEndpointAuthMethodsSupported: ["client_secret_basic"],
    createdAt: new Date(0),
    updatedAt: new Date(0),
  };
}

function remoteSessionClient(id = "rsc-1"): RemoteSessionClient {
  return {
    id,
    projectId: "project-1",
    remoteSessionIssuerId: "rsi-1",
    userSessionIssuerId: "usi-1",
    clientId: "client-id",
    clientIdIssuedAt: new Date(0),
    createdAt: new Date(0),
    updatedAt: new Date(0),
  };
}

function happyServices(linkCalls: LinkToolsetUserSessionIssuerInput[] = []) {
  return {
    resolveUserSessionIssuer: fromPromise<
      UserSessionIssuer | null,
      ResolveUserSessionIssuerInput
    >(async () => null),
    resolveRemoteSessionIssuer: fromPromise<
      RemoteSessionIssuer | null,
      ResolveRemoteSessionIssuerInput
    >(async () => null),
    resolveRemoteSessionClient: fromPromise<
      RemoteSessionClient | null,
      ResolveRemoteSessionClientInput
    >(async () => null),
    createUserSessionIssuer: fromPromise<
      UserSessionIssuer,
      CreateUserSessionIssuerInput
    >(async () => userSessionIssuer()),
    createRemoteSessionIssuer: fromPromise<
      RemoteSessionIssuer,
      CreateRemoteSessionIssuerInput
    >(async () => remoteSessionIssuer()),
    createRemoteSessionClient: fromPromise<
      RemoteSessionClient,
      CreateRemoteSessionClientInput
    >(async ({ input }) => ({
      ...remoteSessionClient(),
      remoteSessionIssuerId: input.remoteSessionIssuer.id,
      userSessionIssuerId: input.userSessionIssuerId,
    })),
    linkToolsetUserSessionIssuer: fromPromise<
      void,
      LinkToolsetUserSessionIssuerInput
    >(async ({ input }) => {
      linkCalls.push(input);
    }),
  };
}

function makeActor(
  input: MigrationInput,
  services: ReturnType<typeof happyServices> = happyServices(),
) {
  return createActor(
    wireUserSessionIssuerMachine.provide({ actors: services }),
    {
      input,
    },
  );
}

describe("wireUserSessionIssuerMachine", () => {
  it("advances custom migrations through user issuer, remote issuer, client, and complete", async () => {
    const linkCalls: LinkToolsetUserSessionIssuerInput[] = [];
    const actor = makeActor(baseInput, happyServices(linkCalls));
    actor.start();

    expect(selectCurrentStep(actor.getSnapshot())?.key).toBe(
      "userSessionIssuer",
    );

    actor.send({ type: "SUBMIT" });
    await waitFor(actor, (state) => state.matches("remoteSessionIssuer"));
    expect(linkCalls).toHaveLength(0);

    actor.send({ type: "SUBMIT" });
    await waitFor(actor, (state) => state.matches("remoteSessionClient"));

    actor.send({
      type: "FORM",
      patch: { clientStrategy: "manual", manualClientId: "manual-client" },
    });
    actor.send({ type: "SUBMIT" });

    await waitFor(actor, (state) => state.matches("complete"));
    expect(selectIsComplete(actor.getSnapshot())).toBe(true);
    expect(selectSteps(actor.getSnapshot()).map((step) => step.status)).toEqual(
      ["done", "done", "done"],
    );
    expect(linkCalls).toEqual([
      { toolsetSlug: "github", userSessionIssuerId: "usi-1" },
    ]);
  });

  it("links Gram-managed migrations immediately after creating the user issuer", async () => {
    const linkCalls: LinkToolsetUserSessionIssuerInput[] = [];
    const actor = makeActor(
      {
        ...baseInput,
        paradigm: "gram",
        defaults: {
          ...defaults,
          proxyProvider: { ...proxyProvider, providerType: "gram" },
        },
      },
      happyServices(linkCalls),
    );
    actor.start();

    actor.send({ type: "SUBMIT" });
    await waitFor(actor, (state) => state.matches("complete"));

    expect(selectSteps(actor.getSnapshot())).toHaveLength(1);
    expect(linkCalls).toEqual([
      { toolsetSlug: "github", userSessionIssuerId: "usi-1" },
    ]);
  });

  it("keeps errors on the same step with form values preserved", async () => {
    const actor = makeActor(baseInput, {
      ...happyServices(),
      createRemoteSessionIssuer: fromPromise<
        RemoteSessionIssuer,
        CreateRemoteSessionIssuerInput
      >(async () => {
        throw new Error("remote boom");
      }),
    });
    actor.start();

    actor.send({ type: "SUBMIT" });
    await waitFor(actor, (state) => state.matches("remoteSessionIssuer"));
    actor.send({
      type: "FORM",
      patch: { remoteSessionIssuerSlug: "custom-slug" },
    });
    actor.send({ type: "SUBMIT" });
    await waitFor(
      actor,
      (state) => selectErrorStep(state)?.key === "remoteSessionIssuer",
    );

    const snapshot = actor.getSnapshot();
    expect(snapshot.matches("remoteSessionIssuer")).toBe(true);
    expect(snapshot.context.form.remoteSessionIssuerSlug).toBe("custom-slug");
    expect(selectErrorStep(snapshot)?.error).toBe("remote boom");
  });

  it("resumes from existing resources supplied on open and still links the toolset", async () => {
    const linkCalls: LinkToolsetUserSessionIssuerInput[] = [];
    const actor = makeActor(
      {
        ...baseInput,
        existingUserSessionIssuer: userSessionIssuer(),
        existingRemoteSessionIssuer: remoteSessionIssuer(),
        existingRemoteSessionClient: remoteSessionClient(),
      },
      happyServices(linkCalls),
    );
    actor.start();

    await waitFor(actor, (state) => state.matches("complete"));
    expect(linkCalls).toEqual([
      { toolsetSlug: "github", userSessionIssuerId: "usi-1" },
    ]);
  });

  it("resumes from edited slugs before creating duplicate resources", async () => {
    const linkCalls: LinkToolsetUserSessionIssuerInput[] = [];
    const createCalls: string[] = [];
    const editedUserIssuer = {
      ...userSessionIssuer("usi-edited"),
      slug: "edited-user",
    };
    const editedRemoteIssuer = {
      ...remoteSessionIssuer("rsi-edited"),
      slug: "edited-remote",
    };
    const editedRemoteClient = {
      ...remoteSessionClient("rsc-edited"),
      remoteSessionIssuerId: "rsi-edited",
      userSessionIssuerId: "usi-edited",
    };

    const actor = makeActor(baseInput, {
      ...happyServices(linkCalls),
      resolveUserSessionIssuer: fromPromise<
        UserSessionIssuer | null,
        ResolveUserSessionIssuerInput
      >(async ({ input }) =>
        input.slug === "edited-user" ? editedUserIssuer : null,
      ),
      resolveRemoteSessionIssuer: fromPromise<
        RemoteSessionIssuer | null,
        ResolveRemoteSessionIssuerInput
      >(async ({ input }) =>
        input.slug === "edited-remote" ? editedRemoteIssuer : null,
      ),
      resolveRemoteSessionClient: fromPromise<
        RemoteSessionClient | null,
        ResolveRemoteSessionClientInput
      >(async ({ input }) =>
        input.userSessionIssuerId === "usi-edited" &&
        input.remoteSessionIssuerId === "rsi-edited"
          ? editedRemoteClient
          : null,
      ),
      createUserSessionIssuer: fromPromise<
        UserSessionIssuer,
        CreateUserSessionIssuerInput
      >(async () => {
        createCalls.push("user");
        return userSessionIssuer();
      }),
      createRemoteSessionIssuer: fromPromise<
        RemoteSessionIssuer,
        CreateRemoteSessionIssuerInput
      >(async () => {
        createCalls.push("remote");
        return remoteSessionIssuer();
      }),
      createRemoteSessionClient: fromPromise<
        RemoteSessionClient,
        CreateRemoteSessionClientInput
      >(async () => {
        createCalls.push("client");
        return remoteSessionClient();
      }),
    });
    actor.start();

    actor.send({
      type: "FORM",
      patch: { userSessionIssuerSlug: "edited-user" },
    });
    actor.send({ type: "SUBMIT" });
    await waitFor(actor, (state) => state.matches("remoteSessionIssuer"));

    actor.send({
      type: "FORM",
      patch: { remoteSessionIssuerSlug: "edited-remote" },
    });
    actor.send({ type: "SUBMIT" });

    await waitFor(actor, (state) => state.matches("complete"));
    expect(createCalls).toEqual([]);
    expect(linkCalls).toEqual([
      { toolsetSlug: "github", userSessionIssuerId: "usi-edited" },
    ]);
  });
});
