import { useFetcher } from "@/contexts/Fetcher";
import {
  useAddExternalOAuthServerMutation,
  useAddOAuthProxyServerMutation,
  useCreateEnvironmentMutation,
  useUpdateOAuthProxyServerMutation,
} from "@gram/client/react-query";
import { useCallback, useState } from "react";
import { toast } from "sonner";

import { extractProxyFormData } from "./reducer";
import type {
  DiscoveredOAuth,
  ProxyFormData,
  WizardDispatch,
  WizardState,
} from "./types";

// ---------------------------------------------------------------------------
// Dependencies
// ---------------------------------------------------------------------------

interface StepActionDeps {
  state: WizardState;
  dispatch: WizardDispatch;
  toolsetSlug: string;
  toolsetName: string;
  activeOrganizationId: string;
  environments: Array<{ name: string }>;
  proxyAudiencePrefilled: string;
  discoveredOAuth: DiscoveredOAuth | null;
  addExternalOAuthMutation: ReturnType<
    typeof useAddExternalOAuthServerMutation
  >;
  addOAuthProxyMutation: ReturnType<typeof useAddOAuthProxyServerMutation>;
  updateOAuthProxyMutation: ReturnType<
    typeof useUpdateOAuthProxyServerMutation
  >;
  createEnvironmentMutation: ReturnType<typeof useCreateEnvironmentMutation>;
}

// ---------------------------------------------------------------------------
// Step actions
// ---------------------------------------------------------------------------

export interface StepActions {
  external_oauth_server_metadata_form: {
    submit: () => void;
  };
  oauth_proxy_server_metadata_form: {
    next: () => void;
    editSubmit: () => void;
    isNextPending: boolean;
  };
  oauth_proxy_client_credentials_form: {
    submit: () => void;
    isSubmitting: boolean;
  };
}

// ---------------------------------------------------------------------------
// Hook
// ---------------------------------------------------------------------------

export function useStepActions(deps: StepActionDeps): StepActions {
  const {
    state,
    dispatch,
    toolsetSlug,
    toolsetName,
    activeOrganizationId,
    environments,
    proxyAudiencePrefilled,
    discoveredOAuth,
    addExternalOAuthMutation,
    addOAuthProxyMutation,
    updateOAuthProxyMutation,
    createEnvironmentMutation,
  } = deps;

  const { fetch: authedFetch } = useFetcher();
  const [isProxyRegisterPending, setIsProxyRegisterPending] = useState(false);

  // --- external_oauth_server_metadata_form ---

  const externalSubmit = useCallback(() => {
    if (state.step !== "external_oauth_server_metadata_form") return;

    let parsedMetadata;
    try {
      parsedMetadata = JSON.parse(state.metadataJson);
    } catch {
      dispatch({
        type: "UPDATE_FIELD",
        field: "jsonError",
        value: "Invalid JSON format",
      });
      return;
    }

    if (!state.slug.trim()) {
      toast.error("Please provide a slug for the OAuth server");
      return;
    }

    const requiredEndpoints = [
      "authorization_endpoint",
      "token_endpoint",
      "registration_endpoint",
    ];
    const missingEndpoints = requiredEndpoints.filter(
      (endpoint) => !parsedMetadata[endpoint],
    );

    if (missingEndpoints.length > 0) {
      dispatch({
        type: "UPDATE_FIELD",
        field: "jsonError",
        value: `Missing required endpoints: ${missingEndpoints.join(", ")}`,
      });
      return;
    }

    dispatch({ type: "UPDATE_FIELD", field: "jsonError", value: "" });
    addExternalOAuthMutation.mutate({
      request: {
        slug: toolsetSlug,
        addExternalOAuthServerRequestBody: {
          externalOauthServer: {
            slug: state.slug,
            metadata: parsedMetadata,
          },
        },
      },
    });
  }, [state, toolsetSlug, dispatch, addExternalOAuthMutation]);

  // --- oauth_proxy_server_metadata_form ---

  const validateProxyForm = useCallback((): boolean => {
    if (state.step !== "oauth_proxy_server_metadata_form") return false;
    dispatch({ type: "SET_ERROR", error: null });

    if (!state.slug.trim()) {
      dispatch({
        type: "SET_ERROR",
        error: "Please provide a slug for the OAuth proxy server",
      });
      return false;
    }
    if (!state.authorizationEndpoint.trim()) {
      dispatch({
        type: "SET_ERROR",
        error: "Authorization endpoint is required",
      });
      return false;
    }
    if (!state.tokenEndpoint.trim()) {
      dispatch({ type: "SET_ERROR", error: "Token endpoint is required" });
      return false;
    }
    const scopesArray = state.scopes
      .split(",")
      .map((s) => s.trim())
      .filter((s) => s.length > 0);
    if (scopesArray.length === 0) {
      dispatch({ type: "SET_ERROR", error: "At least one scope is required" });
      return false;
    }
    return true;
  }, [state, dispatch]);

  const createOAuthProxy = useCallback(
    (proxyFormData: ProxyFormData, clientId: string, clientSecret: string) => {
      const scopesArray = proxyFormData.scopes
        .split(",")
        .map((s) => s.trim())
        .filter((s) => s.length > 0);

      const existingNames = new Set(environments.map((e) => e.name));
      let envName = `${toolsetName} OAuth`;
      if (existingNames.has(envName)) {
        let suffix = 1;
        while (existingNames.has(`${toolsetName} OAuth ${suffix}`)) {
          suffix++;
        }
        envName = `${toolsetName} OAuth ${suffix}`;
      }
      createEnvironmentMutation.mutate(
        {
          request: {
            createEnvironmentForm: {
              name: envName,
              organizationId: activeOrganizationId,
              entries: [
                { name: "CLIENT_ID", value: clientId },
                { name: "CLIENT_SECRET", value: clientSecret },
              ],
            },
          },
        },
        {
          onSuccess: (env) => {
            addOAuthProxyMutation.mutate({
              request: {
                slug: toolsetSlug,
                addOAuthProxyServerRequestBody: {
                  oauthProxyServer: {
                    providerType: "custom",
                    slug: proxyFormData.slug,
                    audience: proxyFormData.audience || undefined,
                    authorizationEndpoint: proxyFormData.authorizationEndpoint,
                    tokenEndpoint: proxyFormData.tokenEndpoint,
                    scopesSupported: scopesArray,
                    tokenEndpointAuthMethodsSupported: [
                      proxyFormData.tokenAuthMethod,
                    ],
                    environmentSlug: env.slug,
                  },
                },
              },
            });
          },
          onError: (error) => {
            console.error("Failed to create environment:", error);
            dispatch({
              type: "SET_RESULT",
              success: false,
              message:
                error instanceof Error
                  ? error.message
                  : "Failed to create environment for OAuth credentials",
            });
          },
        },
      );
    },
    [
      toolsetName,
      toolsetSlug,
      activeOrganizationId,
      environments,
      dispatch,
      createEnvironmentMutation,
      addOAuthProxyMutation,
    ],
  );

  const proxyNext = useCallback(async () => {
    if (!validateProxyForm()) return;

    const registrationEndpoint =
      typeof discoveredOAuth?.metadata.registration_endpoint === "string"
        ? discoveredOAuth.metadata.registration_endpoint
        : null;

    if (
      !registrationEndpoint ||
      state.step !== "oauth_proxy_server_metadata_form"
    ) {
      dispatch({ type: "PROXY_NEXT" });
      return;
    }

    const scopesSupported = state.scopes
      .split(",")
      .map((s) => s.trim())
      .filter((s) => s.length > 0);

    const tokenAuthMethodsSupported = Array.isArray(
      discoveredOAuth?.metadata.token_endpoint_auth_methods_supported,
    )
      ? (discoveredOAuth.metadata
          .token_endpoint_auth_methods_supported as string[])
      : [];

    setIsProxyRegisterPending(true);
    try {
      const response = await authedFetch("/oauth/proxy-register", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          registration_endpoint: registrationEndpoint,
          scopes_supported: scopesSupported,
          token_endpoint_auth_methods_supported: tokenAuthMethodsSupported,
        }),
      });

      if (!response.ok) {
        throw new Error(`Registration failed (HTTP ${response.status})`);
      }

      const result = (await response.json()) as {
        client_id?: string;
        client_secret?: string;
        token_endpoint_auth_method?: string;
      };

      if (!result.client_id) {
        throw new Error("Upstream did not return a client_id");
      }

      const proxyFormData = extractProxyFormData(state);
      if (result.token_endpoint_auth_method) {
        proxyFormData.tokenAuthMethod = result.token_endpoint_auth_method;
      }
      createOAuthProxy(
        proxyFormData,
        result.client_id,
        result.client_secret ?? "",
      );
    } catch (error) {
      toast.error(
        error instanceof Error
          ? `Auto-registration failed: ${error.message}. Enter credentials manually.`
          : "Auto-registration failed. Enter credentials manually.",
      );
      dispatch({ type: "PROXY_NEXT" });
    } finally {
      setIsProxyRegisterPending(false);
    }
  }, [
    validateProxyForm,
    dispatch,
    discoveredOAuth,
    state,
    authedFetch,
    createOAuthProxy,
  ]);

  const proxyEditSubmit = useCallback(() => {
    if (state.step !== "oauth_proxy_server_metadata_form") return;
    if (!validateProxyForm()) return;

    const scopesArray = state.scopes
      .split(",")
      .map((s) => s.trim())
      .filter((s) => s.length > 0);

    const audienceChanged = state.audience !== proxyAudiencePrefilled;

    updateOAuthProxyMutation.mutate({
      request: {
        slug: toolsetSlug,
        updateOAuthProxyServerRequestBody: {
          oauthProxyServer: {
            audience: audienceChanged ? state.audience : undefined,
            authorizationEndpoint: state.authorizationEndpoint,
            tokenEndpoint: state.tokenEndpoint,
            scopesSupported: scopesArray,
            tokenEndpointAuthMethodsSupported: [state.tokenAuthMethod],
            environmentSlug: state.environmentSlug || undefined,
          },
        },
      },
    });
  }, [
    state,
    toolsetSlug,
    proxyAudiencePrefilled,
    validateProxyForm,
    updateOAuthProxyMutation,
  ]);

  // --- oauth_proxy_client_credentials_form ---

  const proxyCreateSubmit = useCallback(() => {
    if (state.step !== "oauth_proxy_client_credentials_form") return;
    dispatch({ type: "SET_ERROR", error: null });

    if (!state.clientId.trim() || !state.clientSecret.trim()) {
      dispatch({
        type: "SET_ERROR",
        error: "Client ID and Client Secret are required",
      });
      return;
    }

    createOAuthProxy(state.proxyFormData, state.clientId, state.clientSecret);
  }, [state, dispatch, createOAuthProxy]);

  const isProxySubmitting =
    createEnvironmentMutation.isPending || addOAuthProxyMutation.isPending;

  return {
    external_oauth_server_metadata_form: {
      submit: externalSubmit,
    },
    oauth_proxy_server_metadata_form: {
      next: proxyNext,
      editSubmit: proxyEditSubmit,
      isNextPending: isProxyRegisterPending || isProxySubmitting,
    },
    oauth_proxy_client_credentials_form: {
      submit: proxyCreateSubmit,
      isSubmitting: isProxySubmitting,
    },
  };
}
