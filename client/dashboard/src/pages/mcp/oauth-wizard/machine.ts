import { assign, fromPromise, setup } from "xstate";

import { checkExternal, checkProxyMeta, checkCreds } from "./guards";
import {
  audienceDirty,
  type Context,
  type DiscoveredOAuth,
  type Input,
  type ProxyDefaults,
  type WizardEvent,
} from "./machine-types";
import {
  nextEnvironmentName,
  type AddExternalOAuthInput,
  type AddOAuthProxyInput,
  type CreateEnvironmentInput,
  type CreateEnvironmentOutput,
  type DeleteEnvironmentInput,
  type UpdateOAuthProxyInput,
} from "./services";

const PROXY_BLANK: ProxyDefaults = {
  slug: "",
  authorizationEndpoint: "",
  tokenEndpoint: "",
  scopes: "",
  audience: "",
  tokenAuthMethod: "client_secret_post",
  environmentSlug: "",
};

function initialProxy(input: Input): Context["proxy"] {
  const defaults = input.editProxyDefaults ?? PROXY_BLANK;
  return {
    ...defaults,
    audiencePrefilled: defaults.audience,
    clientId: "",
    clientSecret: "",
    prefilled: input.editProxyDefaults != null,
  };
}

function initialContext(input: Input): Context {
  return {
    mode: input.mode,
    discovered: input.discovered,
    external: { slug: "", metadataJson: "", jsonError: null, prefilled: false },
    proxy: initialProxy(input),
    envSlug: null,
    error: null,
    result: null,
    toolsetSlug: input.toolsetSlug,
    toolsetName: input.toolsetName,
    activeOrganizationId: input.activeOrganizationId,
    existingEnvNames: input.existingEnvNames,
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

function proxyFieldsFromDiscovered(d: DiscoveredOAuth): Partial<ProxyDefaults> {
  const m = d.metadata;
  const out: Partial<ProxyDefaults> = { slug: d.slug };
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
    updateOAuthProxy: placeholder<UpdateOAuthProxyInput>("updateOAuthProxy"),
  },
  guards: {
    validExternal: ({ context }) => checkExternal(context).ok,
    validProxyMeta: ({ context }) => checkProxyMeta(context).ok,
    validCreds: ({ context }) => checkCreds(context).ok,
  },
  actions: {
    invalidateOnExternalSuccess: () => {},
    invalidateOnProxyCreate: () => {},
    invalidateOnProxyUpdate: () => {},
    captureExternalSuccess: () => {},
    captureProxyCreateSuccess: () => {},
    captureProxyUpdateSuccess: () => {},
  },
}).createMachine({
  id: "oauthWizard",
  context: ({ input }) => initialContext(input),
  initial: "pathSelection",
  states: {
    pathSelection: {
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
      },
    },

    external: {
      initial: "editing",
      states: {
        editing: {
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
            SUBMIT_EDIT: [
              {
                guard: "validProxyMeta",
                target: "updating",
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
        credentials: {
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
                target: "creatingEnvironment",
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
        creatingEnvironment: {
          invoke: {
            src: "createEnvironment",
            input: ({ context }): CreateEnvironmentInput => ({
              organizationId: context.activeOrganizationId,
              name: nextEnvironmentName(
                context.toolsetName,
                context.existingEnvNames,
              ),
              clientId: context.proxy.clientId,
              clientSecret: context.proxy.clientSecret,
            }),
            onDone: {
              target: "creatingProxy",
              actions: assign({
                envSlug: ({ event }) => event.output.envSlug,
              }),
            },
            onError: {
              target: "credentials",
              actions: assign({
                error: ({ event }) =>
                  errorMessage(
                    event.error,
                    "Failed to create environment for OAuth credentials",
                  ),
              }),
            },
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
                  errorMessage(event.error, "Failed to configure OAuth proxy"),
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
            onDone: {
              target: "credentials",
              actions: assign({ envSlug: () => null }),
            },
            onError: {
              target: "fatalError",
              actions: assign({
                error: ({ context, event }) => {
                  const proxyErr = context.error ?? "Proxy creation failed";
                  const cleanupErr = errorMessage(event.error, "unknown error");
                  return `${proxyErr}. Environment cleanup also failed: ${cleanupErr}. The orphaned environment must be deleted manually.`;
                },
              }),
            },
          },
        },
        fatalError: {
          type: "final",
        },
        updating: {
          invoke: {
            src: "updateOAuthProxy",
            input: ({ context }): UpdateOAuthProxyInput => ({
              toolsetSlug: context.toolsetSlug,
              authorizationEndpoint: context.proxy.authorizationEndpoint,
              tokenEndpoint: context.proxy.tokenEndpoint,
              scopes: context.proxy.scopes,
              audience: context.proxy.audience,
              audienceDirty: audienceDirty(context),
              tokenAuthMethod: context.proxy.tokenAuthMethod,
              environmentSlug: context.proxy.environmentSlug,
            }),
            onDone: {
              target: "#oauthWizard.result.success",
              actions: [
                assign({
                  result: () => ({
                    success: true,
                    message:
                      "Your OAuth proxy server has been updated successfully.",
                  }),
                }),
                "captureProxyUpdateSuccess",
                "invalidateOnProxyUpdate",
              ],
            },
            onError: {
              target: "metadata",
              actions: assign({
                error: ({ event }) =>
                  errorMessage(event.error, "Failed to update OAuth proxy"),
              }),
            },
          },
        },
      },
    },

    result: {
      initial: "success",
      states: {
        success: {},
      },
      on: {
        RESET: "#oauthWizard.pathSelection",
      },
    },
  },
});

export type OAuthWizardMachine = typeof oauthWizardMachine;
