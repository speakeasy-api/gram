import type { Toolset } from "@/lib/toolTypes";
import {
  invalidateAllRemoteSessionClients,
  invalidateAllRemoteSessionIssuers,
  invalidateAllToolset,
  invalidateAllUserSessionIssuers,
  useCloneClientFromOAuthProxyProviderMutation,
  useCreateRemoteSessionClientMutation,
  useCreateRemoteSessionIssuerMutation,
  useCreateUserSessionIssuerMutation,
  useDiscoverRemoteSessionIssuerMutation,
  useRegisterRemoteSessionIssuerMutation,
  useRemoteSessionClients,
  useRemoteSessionIssuers,
  useSetToolsetUserSessionIssuerMutation,
  useUserSessionIssuers,
} from "@gram/client/react-query";
import type {
  RemoteSessionIssuer,
  UserSessionIssuer,
} from "@gram/client/models/components";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useMemo, useReducer } from "react";

import { deriveMigrationDefaults, type MigrationDefaults } from "./defaults";

import { getServerURL } from "@/lib/utils";

// Gram's user-session callback URL — has to be present in any upstream
// client's redirect_uri list (RFC 7591 considers it `invalid_client_metadata`
// to register without one). Host comes from the build-time
// __GRAM_SERVER_URL__ constant (see vite.config.ts), so the dashboard agrees
// with the server about its own identity instead of guessing via
// window.location.origin.
export function remoteLoginCallbackURL(): string {
  return `${getServerURL()}/mcp/remote_login_callback`;
}

// useOAuthProxyMigration orchestrates the writes needed to port a toolset from
// the legacy OAuth Proxy paradigm onto user-session world. The shape of the
// migration depends on which OAuth Proxy paradigm the toolset uses today:
//
//   - Gram-managed (providerType === "gram"): Gram is itself the upstream
//     identity provider, so the migration produces only a user_session_issuer.
//     There is no external authorization server for Gram to be an OAuth client
//     of, so no remote_session_issuer / remote_session_client pair is needed.
//
//   - Custom (providerType === "custom"): an external IdP. The migration
//     produces a user_session_issuer plus a remote_session_issuer / client
//     pair. The client step branches three ways — see ClientStrategy below.
//
// Re-entry is idempotent: on mount the hook queries existing rows by slug and
// marks any matching step as already done, so an operator can resume a
// half-finished migration. Strictly orchestration; renders no UI.

export type MigrationStepKey =
  | "userSessionIssuer"
  | "remoteSessionIssuer"
  | "remoteSessionClient";

export type StepStatus = "pending" | "running" | "done" | "error";

export type MigrationParadigm = "gram" | "custom";

// Three ways the remote_session_client can be created:
//   - clone:    Read the upstream client_id / client_secret from the existing
//               oauth_proxy_provider and reuse it. Preserves the IdP-side
//               registration (including its registered redirect URIs).
//   - register: Run RFC 7591 Dynamic Client Registration against the issuer's
//               registration_endpoint. Mints a fresh upstream client. Only
//               available when the issuer advertises a registration endpoint.
//   - manual:   Operator pastes the client_id / client_secret they already
//               registered out-of-band with the upstream IdP.
export type ClientStrategy = "clone" | "register" | "manual";

export type MigrationStep = {
  key: MigrationStepKey;
  /** Short resource name, e.g. "User Session Issuer". */
  resourceLabel: string;
  /** Verb-led summary of what this step does. */
  description: string;
  status: StepStatus;
  error: string | null;
  resultId: string | null;
};

export type MigrationFormState = {
  userSessionIssuerSlug: string;
  remoteSessionIssuerSlug: string;
  issuerUrl: string;
  sessionDurationHours: number;
  /** Which path to use for the remote_session_client step. null = chooser. */
  clientStrategy: ClientStrategy | null;
  /** Gate on the clone path — operator confirms they've registered the new callback. */
  cloneCallbackConfirmed: boolean;
  /** Manual-path inputs. */
  manualClientId: string;
  manualClientSecret: string;
  manualClientName: string;
};

export type UseOAuthProxyMigrationResult =
  | {
      ready: false;
      reason: "no-proxy-provider";
      defaults: null;
    }
  | {
      ready: true;
      paradigm: MigrationParadigm;
      defaults: MigrationDefaults;
      form: MigrationFormState;
      setForm: (patch: Partial<MigrationFormState>) => void;
      steps: MigrationStep[];
      currentStep: MigrationStep | null;
      isComplete: boolean;
      /** The remote_session_issuer this migration is targeting, once it exists. */
      remoteSessionIssuer: RemoteSessionIssuer | null;
      /** The user_session_issuer this migration is targeting, once it exists. */
      userSessionIssuer: UserSessionIssuer | null;
      runCurrentStep: () => Promise<void>;
      reset: () => void;
    };

type InternalState = {
  form: MigrationFormState;
  errors: Partial<Record<MigrationStepKey, string>>;
  running: MigrationStepKey | null;
};

type Action =
  | { type: "set-form"; patch: Partial<MigrationFormState> }
  | { type: "step-start"; key: MigrationStepKey }
  | { type: "step-error"; key: MigrationStepKey; message: string }
  | { type: "step-success"; key: MigrationStepKey }
  | { type: "reset" };

function reducer(state: InternalState, action: Action): InternalState {
  switch (action.type) {
    case "set-form":
      return { ...state, form: { ...state.form, ...action.patch } };
    case "step-start":
      return {
        ...state,
        running: action.key,
        errors: { ...state.errors, [action.key]: undefined },
      };
    case "step-error":
      return {
        ...state,
        running: null,
        errors: { ...state.errors, [action.key]: action.message },
      };
    case "step-success":
      return {
        ...state,
        running: null,
        errors: { ...state.errors, [action.key]: undefined },
      };
    case "reset":
      return { ...state, errors: {}, running: null };
  }
}

export function useOAuthProxyMigration(
  toolset: Toolset,
): UseOAuthProxyMigrationResult {
  const queryClient = useQueryClient();
  const defaults = useMemo(() => deriveMigrationDefaults(toolset), [toolset]);
  const paradigm: MigrationParadigm | null =
    defaults?.proxyProvider.providerType === "gram"
      ? "gram"
      : defaults
        ? "custom"
        : null;

  const initialForm: MigrationFormState = useMemo(
    () => ({
      userSessionIssuerSlug: defaults?.userSessionIssuerSlug ?? "",
      remoteSessionIssuerSlug: defaults?.remoteSessionIssuerSlug ?? "",
      issuerUrl: defaults?.issuerOriginGuess ?? "",
      sessionDurationHours: defaults?.sessionDurationHours ?? 24,
      clientStrategy: null,
      cloneCallbackConfirmed: false,
      manualClientId: "",
      manualClientSecret: "",
      manualClientName: "Gram",
    }),
    [defaults],
  );

  const [state, dispatch] = useReducer(reducer, {
    form: initialForm,
    errors: {},
    running: null,
  });

  const { data: userIssuersData, refetch: refetchUserIssuers } =
    useUserSessionIssuers();
  const { data: remoteIssuersData, refetch: refetchRemoteIssuers } =
    useRemoteSessionIssuers();
  const { data: remoteClientsData, refetch: refetchRemoteClients } =
    useRemoteSessionClients();

  const existingUserIssuer = useMemo(
    () =>
      userIssuersData?.result.items?.find(
        (i) => i.slug === state.form.userSessionIssuerSlug,
      ) ?? null,
    [userIssuersData, state.form.userSessionIssuerSlug],
  );

  const existingRemoteIssuer = useMemo(
    () =>
      remoteIssuersData?.result.items?.find(
        (i) => i.slug === state.form.remoteSessionIssuerSlug,
      ) ?? null,
    [remoteIssuersData, state.form.remoteSessionIssuerSlug],
  );

  const existingRemoteClient = useMemo(() => {
    if (!existingRemoteIssuer || !existingUserIssuer) return null;
    return (
      remoteClientsData?.result.items?.find(
        (c) =>
          c.remoteSessionIssuerId === existingRemoteIssuer.id &&
          c.userSessionIssuerId === existingUserIssuer.id,
      ) ?? null
    );
  }, [remoteClientsData, existingRemoteIssuer, existingUserIssuer]);

  const createUserIssuer = useCreateUserSessionIssuerMutation();
  const discoverRemoteIssuer = useDiscoverRemoteSessionIssuerMutation();
  const createRemoteIssuer = useCreateRemoteSessionIssuerMutation();
  const cloneProvider = useCloneClientFromOAuthProxyProviderMutation();
  const registerRemoteIssuer = useRegisterRemoteSessionIssuerMutation();
  const createRemoteClient = useCreateRemoteSessionClientMutation();
  const setToolsetUSI = useSetToolsetUserSessionIssuerMutation();

  const setForm = useCallback(
    (patch: Partial<MigrationFormState>) =>
      dispatch({ type: "set-form", patch }),
    [],
  );

  const reset = useCallback(() => {
    dispatch({ type: "reset" });
    void refetchUserIssuers();
    void refetchRemoteIssuers();
    void refetchRemoteClients();
  }, [refetchUserIssuers, refetchRemoteIssuers, refetchRemoteClients]);

  const buildSteps = (): MigrationStep[] => {
    const mkStep = (
      key: MigrationStepKey,
      resourceLabel: string,
      description: string,
      existingId: string | null,
    ): MigrationStep => {
      if (existingId) {
        return {
          key,
          resourceLabel,
          description,
          status: "done",
          error: null,
          resultId: existingId,
        };
      }
      if (state.errors[key]) {
        return {
          key,
          resourceLabel,
          description,
          status: "error",
          error: state.errors[key] ?? null,
          resultId: null,
        };
      }
      if (state.running === key) {
        return {
          key,
          resourceLabel,
          description,
          status: "running",
          error: null,
          resultId: null,
        };
      }
      return {
        key,
        resourceLabel,
        description,
        status: "pending",
        error: null,
        resultId: null,
      };
    };

    const usiStep = mkStep(
      "userSessionIssuer",
      "User Session Issuer",
      "Authorization server identity Gram presents to MCP clients. Mints user sessions that MCP clients exchange for access tokens.",
      existingUserIssuer?.id ?? null,
    );

    if (paradigm === "gram") {
      return [usiStep];
    }

    return [
      usiStep,
      mkStep(
        "remoteSessionIssuer",
        "Remote Session Issuer",
        "Upstream authorization server identity Gram speaks OAuth to as a client. Pre-filled by hitting the upstream's RFC 8414 well-known document.",
        existingRemoteIssuer?.id ?? null,
      ),
      mkStep(
        "remoteSessionClient",
        "Remote Session Client",
        "Credentials Gram uses when acting as the remote session issuer's OAuth client. Cloned, registered, or supplied manually depending on the strategy you pick.",
        existingRemoteClient?.id ?? null,
      ),
    ];
  };

  const steps = buildSteps();
  const currentStep = steps.find((s) => s.status !== "done") ?? null;
  const isComplete = currentStep === null;

  const runCurrentStep = useCallback(async () => {
    if (!defaults) return;
    const next = steps.find((s) => s.status !== "done");
    if (!next) return;

    dispatch({ type: "step-start", key: next.key });
    try {
      switch (next.key) {
        case "userSessionIssuer": {
          const usi = await createUserIssuer.mutateAsync({
            request: {
              createUserSessionIssuerForm: {
                slug: state.form.userSessionIssuerSlug,
                authnChallengeMode: "interactive",
                sessionDurationHours: state.form.sessionDurationHours,
              },
            },
          });
          // Link the newly-created user session issuer to this toolset via
          // its FK so the toolset payload (and the "user session issuer
          // wired" indicator that reads it) reflects the linkage. The hook
          // is project-scoped, so without this step the user session issuer
          // exists but is unattached to the toolset.
          await setToolsetUSI.mutateAsync({
            request: {
              slug: toolset.slug,
              setUserSessionIssuerRequestBody: {
                userSessionIssuerId: usi.id,
              },
            },
          });
          await invalidateAllUserSessionIssuers(queryClient);
          await invalidateAllToolset(queryClient);
          await refetchUserIssuers();
          break;
        }
        case "remoteSessionIssuer": {
          // Run discovery first so the issuer row is pre-populated with the
          // upstream's published well-known fields. If discovery fails (no
          // RFC 8414 doc, network error), we still create the row from what
          // we can see on the proxy provider so the migration can proceed.
          let draft: {
            authorizationEndpoint?: string;
            tokenEndpoint?: string;
            registrationEndpoint?: string;
            jwksUri?: string;
            scopesSupported?: string[];
            grantTypesSupported?: string[];
            responseTypesSupported?: string[];
            tokenEndpointAuthMethodsSupported?: string[];
          } = {};
          try {
            const discovered = await discoverRemoteIssuer.mutateAsync({
              request: {
                discoverRemoteSessionIssuerRequestBody: {
                  issuer: state.form.issuerUrl,
                },
              },
            });
            draft = discovered;
          } catch {
            // Fall through with empty draft.
          }

          await createRemoteIssuer.mutateAsync({
            request: {
              createRemoteSessionIssuerForm: {
                slug: state.form.remoteSessionIssuerSlug,
                issuer: state.form.issuerUrl,
                authorizationEndpoint:
                  draft.authorizationEndpoint ??
                  defaults.proxyProvider.authorizationEndpoint,
                tokenEndpoint:
                  draft.tokenEndpoint ?? defaults.proxyProvider.tokenEndpoint,
                registrationEndpoint: draft.registrationEndpoint,
                jwksUri: draft.jwksUri,
                scopesSupported:
                  draft.scopesSupported ??
                  defaults.proxyProvider.scopesSupported ??
                  [],
                grantTypesSupported:
                  draft.grantTypesSupported ??
                  defaults.proxyProvider.grantTypesSupported ??
                  [],
                responseTypesSupported: draft.responseTypesSupported ?? [],
                tokenEndpointAuthMethodsSupported:
                  draft.tokenEndpointAuthMethodsSupported ??
                  defaults.proxyProvider.tokenEndpointAuthMethodsSupported ??
                  [],
              },
            },
          });
          await invalidateAllRemoteSessionIssuers(queryClient);
          await refetchRemoteIssuers();
          break;
        }
        case "remoteSessionClient": {
          if (!existingUserIssuer || !existingRemoteIssuer) {
            throw new Error(
              "previous steps must complete before creating the client",
            );
          }
          switch (state.form.clientStrategy) {
            case "clone": {
              if (!state.form.cloneCallbackConfirmed) {
                throw new Error(
                  "confirm the upstream redirect URI is registered before cloning",
                );
              }
              await cloneProvider.mutateAsync({
                request: {
                  cloneClientFromOAuthProxyProviderForm: {
                    oauthProxyProviderId: defaults.proxyProvider.id,
                    remoteSessionIssuerId: existingRemoteIssuer.id,
                    userSessionIssuerId: existingUserIssuer.id,
                  },
                },
              });
              break;
            }
            case "register": {
              if (!existingRemoteIssuer.registrationEndpoint) {
                throw new Error(
                  "issuer has no registration_endpoint; register is unavailable",
                );
              }
              await registerRemoteIssuer.mutateAsync({
                request: {
                  registerRemoteSessionIssuerForm: {
                    remoteSessionIssuerId: existingRemoteIssuer.id,
                    userSessionIssuerId: existingUserIssuer.id,
                    clientName: state.form.manualClientName || "Gram",
                    // RFC 7591 issuers (e.g. Notion) reject a registration
                    // request with no redirect_uris. Send Gram's
                    // user-session callback so the issued client is usable
                    // from the start.
                    redirectUris: [remoteLoginCallbackURL()],
                  },
                },
              });
              break;
            }
            case "manual": {
              if (!state.form.manualClientId.trim()) {
                throw new Error("client_id is required");
              }
              await createRemoteClient.mutateAsync({
                request: {
                  createRemoteSessionClientForm: {
                    remoteSessionIssuerId: existingRemoteIssuer.id,
                    userSessionIssuerId: existingUserIssuer.id,
                    clientId: state.form.manualClientId.trim(),
                    clientSecret: state.form.manualClientSecret || undefined,
                  },
                },
              });
              break;
            }
            default:
              throw new Error("pick a client strategy first");
          }
          await invalidateAllRemoteSessionClients(queryClient);
          await refetchRemoteClients();
          break;
        }
      }
      dispatch({ type: "step-success", key: next.key });
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      dispatch({ type: "step-error", key: next.key, message });
    }
  }, [
    defaults,
    steps,
    state.form,
    toolset.slug,
    createUserIssuer,
    setToolsetUSI,
    discoverRemoteIssuer,
    createRemoteIssuer,
    cloneProvider,
    registerRemoteIssuer,
    createRemoteClient,
    existingUserIssuer,
    existingRemoteIssuer,
    queryClient,
    refetchUserIssuers,
    refetchRemoteIssuers,
    refetchRemoteClients,
  ]);

  if (!defaults || !paradigm) {
    return { ready: false, reason: "no-proxy-provider", defaults: null };
  }

  return {
    ready: true,
    paradigm,
    defaults,
    form: state.form,
    setForm,
    steps,
    currentStep,
    isComplete,
    remoteSessionIssuer: existingRemoteIssuer,
    userSessionIssuer: existingUserIssuer,
    runCurrentStep,
    reset,
  };
}
