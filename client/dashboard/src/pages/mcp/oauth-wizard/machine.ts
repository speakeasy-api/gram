import { createActorContext } from "@xstate/react";
import { assign, fromPromise, setup, type SnapshotFrom } from "xstate";

import { checkCreds, checkExternal, checkProxyMeta } from "./guards";
import {
  type Context,
  type DiscoveredOAuth,
  type Input,
  type WizardEvent,
} from "./machine-types";
import {
  type AddExternalOAuthInput,
  type AddOAuthProxyInput,
  type CreateEnvironmentInput,
  type CreateEnvironmentOutput,
  type DeleteEnvironmentInput,
  type RegisterClientInput,
  type RegisterClientOutput,
} from "./services";

function initialProxy(): Context["proxy"] {
  return {
    slug: "",
    authorizationEndpoint: "",
    tokenEndpoint: "",
    scopes: "",
    audience: "",
    tokenAuthMethod: "client_secret_basic",
    environmentSlug: "",
    clientId: "",
    clientSecret: "",
    prefilled: false,
  };
}

function initialContext(input: Input): Context {
  return {
    discovered: input.discovered,
    external: { slug: "", metadataJson: "", jsonError: null, prefilled: false },
    proxy: initialProxy(),
    envSlug: null,
    error: null,
    autoRegistering: false,
    result: null,
    toolsetSlug: input.toolsetSlug,
    toolsetName: input.toolsetName,
    activeOrganizationId: input.activeOrganizationId,
  };
}

function externalFromDiscovered(
  d: DiscoveredOAuth,
): Pick<
  Context["external"],
  "slug" | "metadataJson" | "jsonError" | "prefilled"
> {
  return {
    slug: d.slug,
    metadataJson: JSON.stringify(d.metadata, null, 2),
    jsonError: null,
    prefilled: true,
  };
}

function proxyFieldsFromDiscovered(
  d: DiscoveredOAuth,
): Partial<Context["proxy"]> {
  const m = d.metadata;
  const out: Partial<Context["proxy"]> = { slug: d.slug };
  if (typeof m.authorization_endpoint === "string")
    out.authorizationEndpoint = m.authorization_endpoint;
  if (typeof m.token_endpoint === "string")
    out.tokenEndpoint = m.token_endpoint;
  if (Array.isArray(m.scopes_supported))
    out.scopes = m.scopes_supported.join(", ");
  return out;
}

function errorMessage(err: unknown, fallback: string): string {
  return err instanceof Error ? err.message : fallback;
}

function discoveredRegistrationEndpoint(
  d: DiscoveredOAuth | null,
): string | null {
  const v = d?.metadata.registration_endpoint;
  return typeof v === "string" && v.length > 0 ? v : null;
}

function discoveredAuthMethods(d: DiscoveredOAuth | null): string[] {
  const v = d?.metadata.token_endpoint_auth_methods_supported;
  return Array.isArray(v) ? (v as string[]) : [];
}

export function canAutoConfigureFromDiscovered(
  d: DiscoveredOAuth | null,
): boolean {
  if (!d) return false;
  if (discoveredRegistrationEndpoint(d) === null) return false;
  const fields = proxyFieldsFromDiscovered(d);
  // scopes_supported is optional — many MCP servers don't advertise scopes in
  // their well-known doc. DCR and addOAuthProxy both accept empty scopes.
  return Boolean(fields.authorizationEndpoint && fields.tokenEndpoint);
}

function placeholder<TInput, TOutput = void>(name: string) {
  return fromPromise<TOutput, TInput>(async () => {
    throw new Error(
      `[oauthWizardMachine] actor "${name}" not provided. ` +
        `Use machine.provide({ actors: createWizardServices(client) }) at the call site.`,
    );
  });
}

export const oauthWizardMachine = setup({
  types: {
    context: {} as Context,
    events: {} as WizardEvent,
    input: {} as Input,
  },
  actors: {
    addExternalOAuth: placeholder<AddExternalOAuthInput>("addExternalOAuth"),
    createEnvironment: placeholder<
      CreateEnvironmentInput,
      CreateEnvironmentOutput
    >("createEnvironment"),
    addOAuthProxy: placeholder<AddOAuthProxyInput>("addOAuthProxy"),
    deleteEnvironment: placeholder<DeleteEnvironmentInput>("deleteEnvironment"),
    registerClient: placeholder<RegisterClientInput, RegisterClientOutput>(
      "registerClient",
    ),
  },
  guards: {
    validExternal: ({ context }) => checkExternal(context).ok,
    validProxyMeta: ({ context }) => checkProxyMeta(context).ok,
    validCreds: ({ context }) => checkCreds(context).ok,
    canAutoConfigure: ({ context }) =>
      canAutoConfigureFromDiscovered(context.discovered),
  },
  actions: {
    invalidateOnExternalSuccess: () => {},
    invalidateOnProxyCreate: () => {},
    captureExternalSuccess: () => {},
    captureProxyCreateSuccess: () => {},
  },
}).createMachine({
  id: "oauthWizard",
  context: ({ input }) => initialContext(input),
  initial: "pathSelection",
  states: {
    pathSelection: {
      meta: { title: "Connect OAuth" },
      on: {
        SELECT_EXTERNAL: {
          target: "external.editing",
          actions: assign({
            external: ({ context }) =>
              context.discovered && context.discovered.version === "2.1"
                ? externalFromDiscovered(context.discovered)
                : context.external,
          }),
        },
        SELECT_PROXY: {
          target: "proxy.metadata",
          actions: assign({
            proxy: ({ context }) =>
              context.discovered
                ? {
                    ...context.proxy,
                    ...proxyFieldsFromDiscovered(context.discovered),
                    prefilled: true,
                  }
                : context.proxy,
          }),
        },
        SELECT_PROXY_AUTO: {
          guard: "canAutoConfigure",
          target: "proxy.registering",
          actions: assign({
            proxy: ({ context }) =>
              context.discovered
                ? {
                    ...context.proxy,
                    ...proxyFieldsFromDiscovered(context.discovered),
                    prefilled: true,
                  }
                : context.proxy,
            autoRegistering: () => true,
            error: () => null,
          }),
        },
      },
    },

    external: {
      initial: "editing",
      states: {
        editing: {
          meta: { title: "Configure External OAuth" },
          on: {
            FIELD_EXTERNAL: {
              actions: assign({
                external: ({ context, event }) => ({
                  ...context.external,
                  [event.key]: event.value,
                  jsonError:
                    event.key === "metadataJson"
                      ? null
                      : context.external.jsonError,
                }),
              }),
            },
            APPLY_DISCOVERED: {
              guard: ({ context }) => context.discovered != null,
              actions: assign({
                external: ({ context }) =>
                  context.discovered
                    ? externalFromDiscovered(context.discovered)
                    : context.external,
              }),
            },
            SUBMIT: [
              {
                guard: "validExternal",
                target: "submitting",
                actions: assign({
                  external: ({ context }) => ({
                    ...context.external,
                    jsonError: null,
                  }),
                }),
              },
              {
                actions: assign({
                  external: ({ context }) => {
                    const r = checkExternal(context);
                    return {
                      ...context.external,
                      jsonError: r.ok ? null : r.reason,
                    };
                  },
                }),
              },
            ],
            BACK: "#oauthWizard.pathSelection",
          },
        },
        submitting: {
          meta: { title: "Configure External OAuth" },
          invoke: {
            src: "addExternalOAuth",
            input: ({ context }): AddExternalOAuthInput => ({
              toolsetSlug: context.toolsetSlug,
              slug: context.external.slug,
              metadata: JSON.parse(context.external.metadataJson) as Record<
                string,
                unknown
              >,
            }),
            onDone: {
              target: "#oauthWizard.result.success",
              actions: [
                assign({
                  result: () => ({
                    success: true,
                    message:
                      "Your external OAuth server has been configured successfully.",
                  }),
                }),
                "captureExternalSuccess",
                "invalidateOnExternalSuccess",
              ],
            },
            onError: {
              target: "editing",
              actions: assign({
                external: ({ context, event }) => ({
                  ...context.external,
                  jsonError: errorMessage(
                    event.error,
                    "Failed to configure OAuth",
                  ),
                }),
              }),
            },
          },
        },
      },
    },

    proxy: {
      initial: "metadata",
      states: {
        metadata: {
          meta: { title: "Configure OAuth Proxy" },
          on: {
            FIELD_PROXY: {
              actions: assign({
                proxy: ({ context, event }) => ({
                  ...context.proxy,
                  [event.key]: event.value,
                }),
                error: () => null,
              }),
            },
            APPLY_DISCOVERED: {
              guard: ({ context }) => context.discovered != null,
              actions: assign({
                proxy: ({ context }) =>
                  context.discovered
                    ? {
                        ...context.proxy,
                        ...proxyFieldsFromDiscovered(context.discovered),
                        prefilled: true,
                      }
                    : context.proxy,
              }),
            },
            NEXT: [
              {
                guard: "validProxyMeta",
                target: "credentials",
                actions: assign({ error: () => null }),
              },
              {
                actions: assign({
                  error: ({ context }) => {
                    const r = checkProxyMeta(context);
                    return r.ok ? null : r.reason;
                  },
                }),
              },
            ],
            BACK: "#oauthWizard.pathSelection",
          },
        },
        registering: {
          meta: { title: "Configure OAuth Proxy" },
          invoke: {
            src: "registerClient",
            input: ({ context }): RegisterClientInput => ({
              registrationEndpoint:
                discoveredRegistrationEndpoint(context.discovered) ?? "",
              scopesSupported: context.proxy.scopes
                .split(",")
                .map((s) => s.trim())
                .filter((s) => s.length > 0),
              tokenEndpointAuthMethodsSupported: discoveredAuthMethods(
                context.discovered,
              ),
            }),
            onDone: {
              target: "submitting",
              actions: assign({
                proxy: ({ context, event }) => ({
                  ...context.proxy,
                  clientId: event.output.clientId,
                  clientSecret: event.output.clientSecret,
                  tokenAuthMethod:
                    event.output.tokenAuthMethod ??
                    context.proxy.tokenAuthMethod,
                }),
                error: () => null,
              }),
            },
            onError: {
              target: "autoRegisterFailed",
              actions: assign({
                error: ({ event }) =>
                  errorMessage(event.error, "Failed to fetch credentials"),
              }),
            },
          },
        },
        autoRegisterFailed: {
          meta: { title: "OAuth Setup Failed" },
          type: "final",
        },
        credentials: {
          meta: { title: "OAuth Client Credentials" },
          entry: assign({ autoRegistering: () => false }),
          on: {
            FIELD_PROXY: {
              actions: assign({
                proxy: ({ context, event }) => ({
                  ...context.proxy,
                  [event.key]: event.value,
                }),
                error: () => null,
              }),
            },
            SUBMIT: [
              {
                guard: "validCreds",
                target: "submitting",
                actions: assign({ error: () => null }),
              },
              {
                actions: assign({
                  error: ({ context }) => {
                    const r = checkCreds(context);
                    return r.ok ? null : r.reason;
                  },
                }),
              },
            ],
            BACK: "metadata",
          },
        },
        submitting: {
          meta: { title: "OAuth Client Credentials" },
          initial: "creatingEnvironment",
          states: {
            creatingEnvironment: {
              invoke: {
                src: "createEnvironment",
                input: ({ context }): CreateEnvironmentInput => ({
                  organizationId: context.activeOrganizationId,
                  toolsetName: context.toolsetName,
                  clientId: context.proxy.clientId,
                  clientSecret: context.proxy.clientSecret,
                }),
                onDone: {
                  target: "creatingProxy",
                  actions: assign({
                    envSlug: ({ event }) => event.output.envSlug,
                  }),
                },
                onError: [
                  {
                    // Auto-configure path: never drop the user back into the
                    // manual credentials form — they didn't ask to type
                    // anything. Surface the failure on the same screen used
                    // by DCR errors and post-rollback proxy-creation errors.
                    guard: ({ context }) => context.autoRegistering,
                    target: "#oauthWizard.proxy.autoRegisterFailed",
                    actions: assign({
                      error: ({ event }) =>
                        errorMessage(
                          event.error,
                          "Failed to create environment for OAuth credentials",
                        ),
                    }),
                  },
                  {
                    target: "#oauthWizard.proxy.credentials",
                    actions: assign({
                      error: ({ event }) =>
                        errorMessage(
                          event.error,
                          "Failed to create environment for OAuth credentials",
                        ),
                    }),
                  },
                ],
              },
            },
            creatingProxy: {
              invoke: {
                src: "addOAuthProxy",
                input: ({ context }): AddOAuthProxyInput => ({
                  toolsetSlug: context.toolsetSlug,
                  slug: context.proxy.slug,
                  audience: context.proxy.audience,
                  authorizationEndpoint: context.proxy.authorizationEndpoint,
                  tokenEndpoint: context.proxy.tokenEndpoint,
                  scopes: context.proxy.scopes,
                  tokenAuthMethod: context.proxy.tokenAuthMethod,
                  environmentSlug: context.envSlug ?? "",
                }),
                onDone: {
                  target: "#oauthWizard.result.success",
                  actions: [
                    assign({
                      result: () => ({
                        success: true,
                        message:
                          "Your OAuth proxy has been configured successfully. Client credentials have been stored in a new environment.",
                      }),
                    }),
                    "captureProxyCreateSuccess",
                    "invalidateOnProxyCreate",
                  ],
                },
                onError: {
                  target: "rollingBackEnv",
                  actions: assign({
                    error: ({ event }) =>
                      errorMessage(
                        event.error,
                        "Failed to configure OAuth proxy",
                      ),
                  }),
                },
              },
            },
            rollingBackEnv: {
              invoke: {
                src: "deleteEnvironment",
                input: ({ context }): DeleteEnvironmentInput => ({
                  envSlug: context.envSlug ?? "",
                }),
                onDone: [
                  {
                    // Auto-configure path: never drop the user back into the
                    // manual credentials form — they didn't ask to type anything,
                    // and DCR-fetched secrets aren't reusable here. Surface the
                    // proxy-creation error on the same failure screen used by
                    // DCR errors.
                    guard: ({ context }) => context.autoRegistering,
                    target: "#oauthWizard.proxy.autoRegisterFailed",
                    actions: assign({ envSlug: () => null }),
                  },
                  {
                    target: "#oauthWizard.proxy.credentials",
                    actions: assign({ envSlug: () => null }),
                  },
                ],
                onError: {
                  target: "#oauthWizard.proxy.fatalError",
                  actions: assign({
                    error: ({ context, event }) => {
                      const proxyErr = context.error ?? "Proxy creation failed";
                      const cleanupErr = errorMessage(
                        event.error,
                        "unknown error",
                      );
                      return `${proxyErr}. Environment cleanup also failed: ${cleanupErr}. The orphaned environment must be deleted manually.`;
                    },
                  }),
                },
              },
            },
          },
        },
        fatalError: {
          meta: { title: "Configuration Failed" },
          type: "final",
        },
      },
    },

    result: {
      initial: "success",
      states: {
        success: {
          meta: { title: "OAuth Configured" },
        },
      },
      on: {
        RESET: "#oauthWizard.pathSelection",
      },
    },
  },
});

export type OAuthWizardMachine = typeof oauthWizardMachine;
export type WizardSnapshot = SnapshotFrom<typeof oauthWizardMachine>;
export type WizardSend = (event: WizardEvent) => void;

export const WizardContext = createActorContext(oauthWizardMachine);

export function selectWizardTitle(state: WizardSnapshot): string {
  for (const m of Object.values(state.getMeta())) {
    const title = (m as { title?: unknown } | undefined)?.title;
    if (typeof title === "string") return title;
  }
  return "Connect OAuth";
}
