import type { Toolset } from "@/lib/toolTypes";
import {
  invalidateAllRemoteSessionClients,
  invalidateAllRemoteSessionIssuers,
  invalidateAllUserSessionIssuers,
  useCloneClientFromOAuthProxyProviderMutation,
  useCreateRemoteSessionIssuerMutation,
  useCreateUserSessionIssuerMutation,
  useDiscoverRemoteSessionIssuerMutation,
  useRemoteSessionClients,
  useRemoteSessionIssuers,
  useUserSessionIssuers,
} from "@gram/client/react-query";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useMemo, useReducer } from "react";

import {
  canCloneProvider,
  deriveMigrationDefaults,
  type MigrationDefaults,
} from "./defaults";

// useOAuthProxyMigration orchestrates the three writes needed to port a
// toolset from the legacy OAuth Proxy paradigm to user-session world:
//
//   1. ensure a user_session_issuer exists for the project,
//   2. ensure a remote_session_issuer exists (running discovery first so the
//      caller doesn't have to hand-type the well-known fields), and
//   3. mint a remote_session_client by cloning the upstream client_id /
//      client_secret out of the existing oauth_proxy_provider — this keeps
//      the redirect URIs already registered with the upstream IdP usable.
//
// Re-entry is idempotent: on mount the hook queries the existing issuers and
// clients in the project and skips any step that already has a matching row.
// This way an operator can resume a half-finished migration without rolling
// back the partial state. Strictly orchestration; renders no UI.

export type MigrationStepKey =
  | "userSessionIssuer"
  | "remoteSessionIssuer"
  | "remoteSessionClient";

export type StepStatus = "pending" | "skipped" | "running" | "done" | "error";

export type MigrationStep = {
  key: MigrationStepKey;
  label: string;
  status: StepStatus;
  /** Human-readable error from the most recent attempt, if any. */
  error: string | null;
  /** Server-assigned id once the step succeeds (or is detected as already done). */
  resultId: string | null;
};

export type MigrationFormState = {
  userSessionIssuerSlug: string;
  remoteSessionIssuerSlug: string;
  /** Issuer URL the user confirms before we hit the upstream discovery endpoint. */
  issuerUrl: string;
  sessionDurationHours: number;
};

export type UseOAuthProxyMigrationResult =
  | {
      ready: false;
      reason: "no-proxy-provider" | "not-clonable";
      defaults: MigrationDefaults | null;
    }
  | {
      ready: true;
      defaults: MigrationDefaults;
      form: MigrationFormState;
      setForm: (patch: Partial<MigrationFormState>) => void;
      steps: MigrationStep[];
      currentStep: MigrationStep | null;
      isComplete: boolean;
      /** Run the current step. No-op when no step is pending. */
      runCurrentStep: () => Promise<void>;
      /** Reset transient errors and re-detect existing state from the server. */
      reset: () => void;
    };

type InternalState = {
  form: MigrationFormState;
  // Per-step error overrides; success is derived from server state.
  errors: Partial<Record<MigrationStepKey, string>>;
  running: MigrationStepKey | null;
  resetCounter: number;
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
      return {
        ...state,
        errors: {},
        running: null,
        resetCounter: state.resetCounter + 1,
      };
  }
}

export function useOAuthProxyMigration(
  toolset: Toolset,
): UseOAuthProxyMigrationResult {
  const queryClient = useQueryClient();
  const defaults = useMemo(() => deriveMigrationDefaults(toolset), [toolset]);

  // Form state is seeded once from defaults; we always render the hook so
  // listing queries fire regardless of whether the migration is actually
  // applicable. The `ready` flag below gates the rest of the surface.
  const initialForm: MigrationFormState = useMemo(
    () => ({
      userSessionIssuerSlug: defaults?.userSessionIssuerSlug ?? "",
      remoteSessionIssuerSlug: defaults?.remoteSessionIssuerSlug ?? "",
      issuerUrl: defaults?.issuerOriginGuess ?? "",
      sessionDurationHours: defaults?.sessionDurationHours ?? 24,
    }),
    [defaults],
  );

  const [state, dispatch] = useReducer(reducer, {
    form: initialForm,
    errors: {},
    running: null,
    resetCounter: 0,
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
    const stepDef = (
      key: MigrationStepKey,
      label: string,
      existingId: string | null,
    ): MigrationStep => {
      if (existingId) {
        return {
          key,
          label,
          status: "done",
          error: null,
          resultId: existingId,
        };
      }
      if (state.errors[key]) {
        return {
          key,
          label,
          status: "error",
          error: state.errors[key] ?? null,
          resultId: null,
        };
      }
      if (state.running === key) {
        return { key, label, status: "running", error: null, resultId: null };
      }
      return { key, label, status: "pending", error: null, resultId: null };
    };

    return [
      stepDef(
        "userSessionIssuer",
        "Create user session issuer",
        existingUserIssuer?.id ?? null,
      ),
      stepDef(
        "remoteSessionIssuer",
        "Create remote session issuer",
        existingRemoteIssuer?.id ?? null,
      ),
      stepDef(
        "remoteSessionClient",
        "Clone OAuth proxy client",
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
          await createUserIssuer.mutateAsync({
            request: {
              createUserSessionIssuerForm: {
                slug: state.form.userSessionIssuerSlug,
                authnChallengeMode: "interactive",
                sessionDurationHours: state.form.sessionDurationHours,
              },
            },
          });
          await invalidateAllUserSessionIssuers(queryClient);
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
              "previous steps must complete before cloning the client",
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
    createUserIssuer,
    discoverRemoteIssuer,
    createRemoteIssuer,
    cloneProvider,
    existingUserIssuer,
    existingRemoteIssuer,
    queryClient,
    refetchUserIssuers,
    refetchRemoteIssuers,
    refetchRemoteClients,
  ]);

  if (!defaults) {
    return { ready: false, reason: "no-proxy-provider", defaults: null };
  }
  if (!canCloneProvider(defaults.proxyProvider)) {
    return { ready: false, reason: "not-clonable", defaults };
  }

  return {
    ready: true,
    defaults,
    form: state.form,
    setForm,
    steps,
    currentStep,
    isComplete,
    runCurrentStep,
    reset,
  };
}
