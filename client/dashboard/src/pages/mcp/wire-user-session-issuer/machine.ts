import { createActorContext } from "@xstate/react";
import { assign, fromPromise, setup, type SnapshotFrom } from "xstate";

import type {
  CreateRemoteSessionClientInput,
  CreateRemoteSessionIssuerInput,
  CreateUserSessionIssuerInput,
  LinkToolsetUserSessionIssuerInput,
  MigrationContext,
  MigrationEvent,
  MigrationFormState,
  MigrationInput,
  MigrationStep,
  MigrationStepKey,
  ResolveRemoteSessionClientInput,
  ResolveRemoteSessionIssuerInput,
  ResolveUserSessionIssuerInput,
} from "./machine-types";

function initialForm(input: MigrationInput): MigrationFormState {
  return {
    userSessionIssuerSlug: input.defaults.userSessionIssuerSlug,
    remoteSessionIssuerSlug: input.defaults.remoteSessionIssuerSlug,
    issuerUrl: input.defaults.issuerOriginGuess ?? "",
    sessionDurationHours: input.defaults.sessionDurationHours,
    clientStrategy: null,
    cloneCallbackConfirmed: false,
    manualClientId: "",
    manualClientSecret: "",
    manualClientName: "Speakeasy",
  };
}

function initialContext(input: MigrationInput): MigrationContext {
  return {
    defaults: input.defaults,
    paradigm: input.paradigm,
    toolsetSlug: input.toolsetSlug,
    form: initialForm(input),
    userSessionIssuer: input.existingUserSessionIssuer ?? null,
    remoteSessionIssuer: input.existingRemoteSessionIssuer ?? null,
    remoteSessionClient: input.existingRemoteSessionClient ?? null,
    toolsetLinked: input.toolsetLinked ?? false,
    error: null,
    errorStep: null,
  };
}

function errorMessage(err: unknown, fallback: string): string {
  return err instanceof Error ? err.message : fallback;
}

function placeholder<TInput, TOutput = void>(name: string) {
  return fromPromise<TOutput, TInput>(async () => {
    throw new Error(
      `[wireUserSessionIssuerMachine] actor "${name}" not provided. ` +
        `Use machine.provide({ actors: createMigrationServices(client) }) at the call site.`,
    );
  });
}

const STEP_META: Record<
  MigrationStepKey,
  Pick<MigrationStep, "resourceLabel" | "description">
> = {
  userSessionIssuer: {
    resourceLabel: "User Session Issuer",
    description:
      "Authorization server identity Gram presents to MCP clients. Mints user sessions that MCP clients exchange for access tokens.",
  },
  remoteSessionIssuer: {
    resourceLabel: "Remote Session Issuer",
    description:
      "Upstream authorization server identity Gram speaks OAuth to as a client. Pre-filled by hitting the upstream's RFC 8414 well-known document.",
  },
  remoteSessionClient: {
    resourceLabel: "Remote Session Client",
    description:
      "Credentials Gram uses when acting as the remote session issuer's OAuth client. Cloned, registered, or supplied manually depending on the strategy you pick.",
  },
};

export const wireUserSessionIssuerMachine = setup({
  types: {
    context: {} as MigrationContext,
    events: {} as MigrationEvent,
    input: {} as MigrationInput,
  },
  actors: {
    resolveUserSessionIssuer: placeholder<
      ResolveUserSessionIssuerInput,
      MigrationContext["userSessionIssuer"]
    >("resolveUserSessionIssuer"),
    resolveRemoteSessionIssuer: placeholder<
      ResolveRemoteSessionIssuerInput,
      MigrationContext["remoteSessionIssuer"]
    >("resolveRemoteSessionIssuer"),
    resolveRemoteSessionClient: placeholder<
      ResolveRemoteSessionClientInput,
      MigrationContext["remoteSessionClient"]
    >("resolveRemoteSessionClient"),
    createUserSessionIssuer: placeholder<
      CreateUserSessionIssuerInput,
      NonNullable<MigrationContext["userSessionIssuer"]>
    >("createUserSessionIssuer"),
    createRemoteSessionIssuer: placeholder<
      CreateRemoteSessionIssuerInput,
      NonNullable<MigrationContext["remoteSessionIssuer"]>
    >("createRemoteSessionIssuer"),
    createRemoteSessionClient: placeholder<
      CreateRemoteSessionClientInput,
      NonNullable<MigrationContext["remoteSessionClient"]>
    >("createRemoteSessionClient"),
    linkToolsetUserSessionIssuer:
      placeholder<LinkToolsetUserSessionIssuerInput>(
        "linkToolsetUserSessionIssuer",
      ),
  },
  guards: {
    isGram: ({ context }) => context.paradigm === "gram",
    hasUserSessionIssuer: ({ context }) => context.userSessionIssuer !== null,
    hasRemoteSessionIssuer: ({ context }) =>
      context.remoteSessionIssuer !== null,
    hasRemoteSessionClient: ({ context }) =>
      context.remoteSessionClient !== null,
    isComplete: ({ context }) => {
      if (!context.toolsetLinked) return false;
      if (!context.userSessionIssuer) return false;
      if (context.paradigm === "gram") return true;
      if (!context.remoteSessionIssuer) return false;
      if (!context.remoteSessionClient) return false;
      return true;
    },
    canCreateRemoteSessionClient: ({ context }) => {
      if (!context.userSessionIssuer || !context.remoteSessionIssuer) {
        return false;
      }
      switch (context.form.clientStrategy) {
        case "clone":
          return context.form.cloneCallbackConfirmed;
        case "register":
          return !!context.remoteSessionIssuer.registrationEndpoint;
        case "manual":
          return context.form.manualClientId.trim().length > 0;
        case null:
          return false;
      }
    },
  },
  actions: {
    invalidateOnUserSessionIssuerCreate: () => {},
    invalidateOnRemoteSessionIssuerCreate: () => {},
    invalidateOnRemoteSessionClientCreate: () => {},
    invalidateOnToolsetLink: () => {},
  },
}).createMachine({
  id: "wireUserSessionIssuer",
  context: ({ input }) => initialContext(input),
  initial: "routing",
  on: {
    FORM: {
      actions: assign({
        form: ({ context, event }) => ({
          ...context.form,
          ...event.patch,
        }),
      }),
    },
  },
  states: {
    // Routes freshly-seeded or resumed context to the first incomplete step.
    // This keeps resource existence, final toolset linkage, and Gram/custom
    // branching in one place instead of duplicating it across every submit.
    routing: {
      always: [
        {
          guard: "isComplete",
          target: "complete",
        },
        {
          guard: ({ context }) => context.userSessionIssuer === null,
          target: "userSessionIssuer",
        },
        {
          guard: "isGram",
          target: "linkingUserSessionIssuer",
        },
        {
          guard: ({ context }) => context.remoteSessionIssuer === null,
          target: "remoteSessionIssuer",
        },
        {
          guard: ({ context }) => context.remoteSessionClient === null,
          target: "resolvingRemoteSessionClient",
        },
        {
          target: "linkingRemoteSessionClient",
        },
      ],
    },

    userSessionIssuer: {
      on: {
        SUBMIT: [
          {
            guard: "hasUserSessionIssuer",
            target: "linkingUserSessionIssuer",
            actions: assign({ error: () => null, errorStep: () => null }),
          },
          {
            target: "resolvingUserSessionIssuer",
            actions: assign({ error: () => null, errorStep: () => null }),
          },
        ],
      },
    },

    resolvingUserSessionIssuer: {
      invoke: {
        src: "resolveUserSessionIssuer",
        input: ({ context }): ResolveUserSessionIssuerInput => ({
          slug: context.form.userSessionIssuerSlug,
        }),
        onDone: [
          {
            guard: ({ event }) => event.output !== null,
            target: "routing",
            actions: assign({
              userSessionIssuer: ({ event }) => event.output,
              error: () => null,
              errorStep: () => null,
            }),
          },
          {
            target: "creatingUserSessionIssuer",
          },
        ],
        onError: {
          target: "userSessionIssuer",
          actions: assign({
            error: ({ event }) =>
              errorMessage(
                event.error,
                "Failed to resolve user session issuer",
              ),
            errorStep: () => "userSessionIssuer" as const,
          }),
        },
      },
    },

    creatingUserSessionIssuer: {
      invoke: {
        src: "createUserSessionIssuer",
        input: ({ context }): CreateUserSessionIssuerInput => ({
          slug: context.form.userSessionIssuerSlug,
          sessionDurationHours: context.form.sessionDurationHours,
        }),
        onDone: [
          {
            guard: "isGram",
            target: "linkingUserSessionIssuer",
            actions: [
              assign({
                userSessionIssuer: ({ event }) => event.output,
              }),
              "invalidateOnUserSessionIssuerCreate",
            ],
          },
          {
            target: "remoteSessionIssuer",
            actions: [
              assign({
                userSessionIssuer: ({ event }) => event.output,
              }),
              "invalidateOnUserSessionIssuerCreate",
            ],
          },
        ],
        onError: {
          target: "userSessionIssuer",
          actions: assign({
            error: ({ event }) =>
              errorMessage(event.error, "Failed to create user session issuer"),
            errorStep: () => "userSessionIssuer" as const,
          }),
        },
      },
    },

    linkingUserSessionIssuer: {
      invoke: {
        src: "linkToolsetUserSessionIssuer",
        input: ({ context }): LinkToolsetUserSessionIssuerInput => {
          if (!context.userSessionIssuer) {
            throw new Error("user session issuer is required");
          }
          return {
            toolsetSlug: context.toolsetSlug,
            userSessionIssuerId: context.userSessionIssuer.id,
          };
        },
        onDone: {
          target: "complete",
          actions: [
            assign({
              toolsetLinked: () => true,
              error: () => null,
              errorStep: () => null,
            }),
            "invalidateOnToolsetLink",
          ],
        },
        onError: {
          target: "userSessionIssuer",
          actions: assign({
            error: ({ event }) =>
              errorMessage(event.error, "Failed to link user session issuer"),
            errorStep: () => "userSessionIssuer" as const,
          }),
        },
      },
    },

    remoteSessionIssuer: {
      on: {
        SUBMIT: [
          {
            guard: "hasRemoteSessionIssuer",
            target: "resolvingRemoteSessionClient",
            actions: assign({ error: () => null, errorStep: () => null }),
          },
          {
            target: "resolvingRemoteSessionIssuer",
            actions: assign({ error: () => null, errorStep: () => null }),
          },
        ],
      },
    },

    resolvingRemoteSessionIssuer: {
      invoke: {
        src: "resolveRemoteSessionIssuer",
        input: ({ context }): ResolveRemoteSessionIssuerInput => ({
          slug: context.form.remoteSessionIssuerSlug,
        }),
        onDone: [
          {
            guard: ({ event }) => event.output !== null,
            target: "resolvingRemoteSessionClient",
            actions: assign({
              remoteSessionIssuer: ({ event }) => event.output,
              error: () => null,
              errorStep: () => null,
            }),
          },
          {
            target: "creatingRemoteSessionIssuer",
          },
        ],
        onError: {
          target: "remoteSessionIssuer",
          actions: assign({
            error: ({ event }) =>
              errorMessage(
                event.error,
                "Failed to resolve remote session issuer",
              ),
            errorStep: () => "remoteSessionIssuer" as const,
          }),
        },
      },
    },

    creatingRemoteSessionIssuer: {
      invoke: {
        src: "createRemoteSessionIssuer",
        input: ({ context }): CreateRemoteSessionIssuerInput => ({
          slug: context.form.remoteSessionIssuerSlug,
          issuerUrl: context.form.issuerUrl,
          proxyProvider: context.defaults.proxyProvider,
        }),
        onDone: {
          target: "resolvingRemoteSessionClient",
          actions: [
            assign({
              remoteSessionIssuer: ({ event }) => event.output,
            }),
            "invalidateOnRemoteSessionIssuerCreate",
          ],
        },
        onError: {
          target: "remoteSessionIssuer",
          actions: assign({
            error: ({ event }) =>
              errorMessage(
                event.error,
                "Failed to create remote session issuer",
              ),
            errorStep: () => "remoteSessionIssuer" as const,
          }),
        },
      },
    },

    remoteSessionClient: {
      on: {
        SUBMIT: [
          {
            guard: "hasRemoteSessionClient",
            target: "linkingRemoteSessionClient",
            actions: assign({ error: () => null, errorStep: () => null }),
          },
          {
            guard: "canCreateRemoteSessionClient",
            target: "creatingRemoteSessionClient",
            actions: assign({ error: () => null, errorStep: () => null }),
          },
          {
            actions: assign({
              error: () => "pick a client strategy first",
              errorStep: () => "remoteSessionClient" as const,
            }),
          },
        ],
      },
    },

    resolvingRemoteSessionClient: {
      invoke: {
        src: "resolveRemoteSessionClient",
        input: ({ context }): ResolveRemoteSessionClientInput => {
          if (!context.userSessionIssuer || !context.remoteSessionIssuer) {
            throw new Error(
              "previous steps must complete before resolving the client",
            );
          }
          return {
            userSessionIssuerId: context.userSessionIssuer.id,
            remoteSessionIssuerId: context.remoteSessionIssuer.id,
          };
        },
        onDone: [
          {
            guard: ({ event }) => event.output !== null,
            target: "linkingRemoteSessionClient",
            actions: assign({
              remoteSessionClient: ({ event }) => event.output,
              error: () => null,
              errorStep: () => null,
            }),
          },
          {
            target: "remoteSessionClient",
          },
        ],
        onError: {
          target: "remoteSessionClient",
          actions: assign({
            error: ({ event }) =>
              errorMessage(
                event.error,
                "Failed to resolve remote session client",
              ),
            errorStep: () => "remoteSessionClient" as const,
          }),
        },
      },
    },

    creatingRemoteSessionClient: {
      invoke: {
        src: "createRemoteSessionClient",
        input: ({ context }): CreateRemoteSessionClientInput => {
          if (
            !context.userSessionIssuer ||
            !context.remoteSessionIssuer ||
            !context.form.clientStrategy
          ) {
            throw new Error(
              "previous steps must complete before creating the client",
            );
          }
          return {
            userSessionIssuerId: context.userSessionIssuer.id,
            remoteSessionIssuer: context.remoteSessionIssuer,
            proxyProviderId: context.defaults.proxyProvider.id,
            strategy: context.form.clientStrategy,
            cloneCallbackConfirmed: context.form.cloneCallbackConfirmed,
            manualClientId: context.form.manualClientId,
            manualClientSecret: context.form.manualClientSecret,
            manualClientName: context.form.manualClientName,
          };
        },
        onDone: {
          target: "linkingRemoteSessionClient",
          actions: [
            assign({
              remoteSessionClient: ({ event }) => event.output,
            }),
            "invalidateOnRemoteSessionClientCreate",
          ],
        },
        onError: {
          target: "remoteSessionClient",
          actions: assign({
            error: ({ event }) =>
              errorMessage(
                event.error,
                "Failed to create remote session client",
              ),
            errorStep: () => "remoteSessionClient" as const,
          }),
        },
      },
    },

    linkingRemoteSessionClient: {
      invoke: {
        src: "linkToolsetUserSessionIssuer",
        input: ({ context }): LinkToolsetUserSessionIssuerInput => {
          if (!context.userSessionIssuer) {
            throw new Error("user session issuer is required");
          }
          return {
            toolsetSlug: context.toolsetSlug,
            userSessionIssuerId: context.userSessionIssuer.id,
          };
        },
        onDone: {
          target: "complete",
          actions: [
            assign({
              toolsetLinked: () => true,
              error: () => null,
              errorStep: () => null,
            }),
            "invalidateOnToolsetLink",
          ],
        },
        onError: {
          target: "remoteSessionClient",
          actions: assign({
            error: ({ event }) =>
              errorMessage(event.error, "Failed to link user session issuer"),
            errorStep: () => "remoteSessionClient" as const,
          }),
        },
      },
    },

    complete: {},
  },
});

export type WireUserSessionIssuerMachine = typeof wireUserSessionIssuerMachine;
export type WireUserSessionIssuerSnapshot = SnapshotFrom<
  typeof wireUserSessionIssuerMachine
>;

export const WireUserSessionIssuerContext = createActorContext(
  wireUserSessionIssuerMachine,
);

export function selectSteps(
  state: WireUserSessionIssuerSnapshot,
): MigrationStep[] {
  const stepKeys: MigrationStepKey[] =
    state.context.paradigm === "gram"
      ? ["userSessionIssuer"]
      : ["userSessionIssuer", "remoteSessionIssuer", "remoteSessionClient"];

  return stepKeys.map((key) => ({
    key,
    ...STEP_META[key],
    status: selectStepStatus(state, key),
    error: state.context.errorStep === key ? state.context.error : null,
    resultId: selectStepResultId(state.context, key),
  }));
}

export function selectCurrentStep(
  state: WireUserSessionIssuerSnapshot,
): MigrationStep | null {
  if (state.matches("complete")) return null;
  return selectSteps(state).find((step) => step.status !== "done") ?? null;
}

export function selectIsComplete(
  state: WireUserSessionIssuerSnapshot,
): boolean {
  return state.matches("complete");
}

export function selectActiveIndex(
  state: WireUserSessionIssuerSnapshot,
): number {
  const steps = selectSteps(state);
  if (steps.length === 0) return 0;
  if (selectIsComplete(state)) return steps.length - 1;
  const current = selectCurrentStep(state);
  return Math.max(
    0,
    steps.findIndex((step) => step.key === current?.key),
  );
}

export function selectCanAdvance(
  state: WireUserSessionIssuerSnapshot,
): boolean {
  if (selectIsComplete(state) || selectRunningStep(state)) return false;

  const current = selectCurrentStep(state);
  if (current?.key !== "remoteSessionClient") return true;
  if (state.context.remoteSessionClient) return true;

  const { form, remoteSessionIssuer } = state.context;
  switch (form.clientStrategy) {
    case "clone":
      return form.cloneCallbackConfirmed;
    case "register":
      return !!remoteSessionIssuer?.registrationEndpoint;
    case "manual":
      return form.manualClientId.trim().length > 0;
    case null:
      return false;
  }
}

export function selectRunningStep(
  state: WireUserSessionIssuerSnapshot,
): MigrationStep | null {
  return selectSteps(state).find((step) => step.status === "running") ?? null;
}

export function selectErrorStep(
  state: WireUserSessionIssuerSnapshot,
): MigrationStep | null {
  return selectSteps(state).find((step) => step.status === "error") ?? null;
}

function selectStepStatus(
  state: WireUserSessionIssuerSnapshot,
  key: MigrationStepKey,
): MigrationStep["status"] {
  if (isStepRunning(state, key)) return "running";
  if (state.context.errorStep === key && state.context.error) return "error";
  if (selectStepResultId(state.context, key)) return "done";
  return "pending";
}

function isStepRunning(
  state: WireUserSessionIssuerSnapshot,
  key: MigrationStepKey,
): boolean {
  switch (key) {
    case "userSessionIssuer":
      return (
        state.matches("creatingUserSessionIssuer") ||
        state.matches("linkingUserSessionIssuer")
      );
    case "remoteSessionIssuer":
      return state.matches("creatingRemoteSessionIssuer");
    case "remoteSessionClient":
      return (
        state.matches("creatingRemoteSessionClient") ||
        state.matches("linkingRemoteSessionClient")
      );
  }
}

function selectStepResultId(
  context: MigrationContext,
  key: MigrationStepKey,
): string | null {
  switch (key) {
    case "userSessionIssuer":
      return context.userSessionIssuer?.id ?? null;
    case "remoteSessionIssuer":
      return context.remoteSessionIssuer?.id ?? null;
    case "remoteSessionClient":
      return context.remoteSessionClient?.id ?? null;
  }
}
