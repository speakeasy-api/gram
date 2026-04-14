import {
  useAddExternalOAuthServerMutation,
  useAddOAuthProxyServerMutation,
  useCreateEnvironmentMutation,
  useUpdateEnvironmentMutation,
  useUpdateOAuthProxyServerMutation,
} from "@gram/client/react-query";
import { useCallback } from "react";
import { toast } from "sonner";

import type { WizardDispatch, WizardState } from "./types";

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
  addExternalOAuthMutation: ReturnType<
    typeof useAddExternalOAuthServerMutation
  >;
  addOAuthProxyMutation: ReturnType<typeof useAddOAuthProxyServerMutation>;
  updateOAuthProxyMutation: ReturnType<
    typeof useUpdateOAuthProxyServerMutation
  >;
  createEnvironmentMutation: ReturnType<typeof useCreateEnvironmentMutation>;
  updateEnvironmentMutation: ReturnType<typeof useUpdateEnvironmentMutation>;
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
    addExternalOAuthMutation,
    addOAuthProxyMutation,
    updateOAuthProxyMutation,
    createEnvironmentMutation,
    updateEnvironmentMutation,
  } = deps;

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

  const proxyNext = useCallback(() => {
    if (!validateProxyForm()) return;
    dispatch({ type: "PROXY_NEXT" });
  }, [validateProxyForm, dispatch]);

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

    const { proxyFormData } = state;
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
            entries: [],
          },
        },
      },
      {
        onSuccess: (env) => {
          updateEnvironmentMutation.mutate(
            {
              request: {
                slug: env.slug,
                updateEnvironmentRequestBody: {
                  entriesToUpdate: [
                    { name: "CLIENT_ID", value: state.clientId },
                    { name: "CLIENT_SECRET", value: state.clientSecret },
                  ],
                  entriesToRemove: [],
                },
              },
            },
            {
              onSuccess: () => {
                addOAuthProxyMutation.mutate({
                  request: {
                    slug: toolsetSlug,
                    addOAuthProxyServerRequestBody: {
                      oauthProxyServer: {
                        providerType: "custom",
                        slug: proxyFormData.slug,
                        audience: proxyFormData.audience || undefined,
                        authorizationEndpoint:
                          proxyFormData.authorizationEndpoint,
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
                console.error("Failed to store OAuth credentials:", error);
                dispatch({
                  type: "SET_RESULT",
                  success: false,
                  message: "Failed to store OAuth credentials",
                });
              },
            },
          );
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
  }, [
    state,
    toolsetName,
    toolsetSlug,
    activeOrganizationId,
    environments,
    dispatch,
    createEnvironmentMutation,
    updateEnvironmentMutation,
    addOAuthProxyMutation,
  ]);

  const isProxySubmitting =
    createEnvironmentMutation.isPending ||
    updateEnvironmentMutation.isPending ||
    addOAuthProxyMutation.isPending;

  return {
    external_oauth_server_metadata_form: {
      submit: externalSubmit,
    },
    oauth_proxy_server_metadata_form: {
      next: proxyNext,
      editSubmit: proxyEditSubmit,
    },
    oauth_proxy_client_credentials_form: {
      submit: proxyCreateSubmit,
      isSubmitting: isProxySubmitting,
    },
  };
}
