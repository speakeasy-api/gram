import { FeatureRequestModal } from "@/components/FeatureRequestModal";
import { Dialog } from "@/components/ui/dialog";
import { useSession } from "@/contexts/Auth";
import { useTelemetry } from "@/contexts/Telemetry";
import { Toolset } from "@/lib/toolTypes";
import { getServerURL } from "@/lib/utils";
import { useProductTier } from "@/hooks/useProductTier";
import { useRoutes } from "@/routes";
import {
  invalidateAllGetMcpMetadata,
  invalidateAllListEnvironments,
  invalidateAllToolset,
  useAddExternalOAuthServerMutation,
  useAddOAuthProxyServerMutation,
  useUpdateOAuthProxyServerMutation,
  useCreateEnvironmentMutation,
  useGetMcpMetadata,
  useListEnvironments,
  useUpdateEnvironmentMutation,
} from "@gram/client/react-query";
import { useQueryClient } from "@tanstack/react-query";
import { Globe } from "lucide-react";
import { useCallback, useEffect, useMemo, useReducer, useRef } from "react";
import { toast } from "sonner";

import { ExternalOAuthForm } from "./ExternalOAuthForm";
import { PathSelection } from "./PathSelection";
import { ProxyCredentialsForm } from "./ProxyCredentialsForm";
import { ProxyMetadataForm } from "./ProxyMetadataForm";
import { INITIAL_STATE, wizardReducer } from "./reducer";
import type { DiscoveredOAuth } from "./types";

// ---------------------------------------------------------------------------
// Container
// ---------------------------------------------------------------------------

function OAuthWizard({
  isOpen,
  onClose,
  toolsetSlug,
  toolset,
  onSuccess,
  editMode,
}: {
  isOpen: boolean;
  onClose: () => void;
  toolsetSlug: string;
  toolset: Toolset;
  onSuccess: () => void;
  editMode?: { proxyServer: NonNullable<Toolset["oauthProxyServer"]> };
}) {
  const discoveredOAuth = useMemo<DiscoveredOAuth | null>(() => {
    const baseURL = getServerURL();
    const mcpSlug = toolset.mcpSlug;
    for (const tool of toolset.rawTools) {
      const def = tool.externalMcpToolDefinition;
      if (!def?.requiresOauth) continue;
      if (!def.oauthAuthorizationEndpoint && !def.oauthTokenEndpoint) continue;

      const metadata: Record<string, unknown> = {
        issuer: `${baseURL}/mcp/${mcpSlug}`,
        response_types_supported: ["code"],
        grant_types_supported: ["authorization_code", "refresh_token"],
        code_challenge_methods_supported: ["S256"],
      };
      if (def.oauthAuthorizationEndpoint)
        metadata.authorization_endpoint = def.oauthAuthorizationEndpoint;
      if (def.oauthTokenEndpoint)
        metadata.token_endpoint = def.oauthTokenEndpoint;
      if (def.oauthRegistrationEndpoint)
        metadata.registration_endpoint = def.oauthRegistrationEndpoint;
      if (def.oauthScopesSupported?.length)
        metadata.scopes_supported = def.oauthScopesSupported;

      return {
        slug: def.slug,
        name: def.registryServerName,
        version: def.oauthVersion,
        metadata,
      };
    }
    return null;
  }, [toolset.rawTools, toolset.mcpSlug]);

  const [state, dispatch] = useReducer(wizardReducer, INITIAL_STATE);

  // Snapshot the prefilled audience so we can detect whether the user actually
  // changed it on submit. Without this, opening the edit modal on a proxy
  // whose audience is NULL would silently submit `audience: ""` (because the
  // form prefills empty-string for null DB values), mutating NULL → "" on the
  // server.
  const proxyAudiencePrefilledRef = useRef<string>("");

  // Pre-fill from editMode whenever the underlying proxy server data changes.
  const editProxyServer = editMode?.proxyServer;
  useEffect(() => {
    if (!editProxyServer) return;
    const provider = editProxyServer.oauthProxyProviders?.[0];
    const initialAudience = editProxyServer.audience ?? "";
    proxyAudiencePrefilledRef.current = initialAudience;
    dispatch({
      type: "SELECT_PROXY",
      defaults: {
        slug: editProxyServer.slug ?? "",
        audience: initialAudience,
        authorizationEndpoint: provider?.authorizationEndpoint ?? "",
        tokenEndpoint: provider?.tokenEndpoint ?? "",
        scopes: (provider?.scopesSupported ?? []).join(", "),
        tokenAuthMethod:
          provider?.tokenEndpointAuthMethodsSupported?.[0] ??
          "client_secret_post",
        environmentSlug: provider?.environmentSlug ?? "",
      },
    });
  }, [editProxyServer]);

  // Reset wizard state when the modal closes.
  useEffect(() => {
    if (!isOpen) {
      dispatch({ type: "RESET" });
    }
  }, [isOpen]);

  const telemetry = useTelemetry();
  const queryClient = useQueryClient();
  const session = useSession();

  const hasMultipleOAuth2AuthCode =
    toolset.oauthEnablementMetadata?.oauth2SecurityCount > 1;

  // --- Mutations ---

  const addExternalOAuthMutation = useAddExternalOAuthServerMutation({
    onSuccess: () => {
      invalidateAllToolset(queryClient);
      telemetry.capture("mcp_event", {
        action: "external_oauth_configured",
        slug: toolsetSlug,
      });
      onSuccess();
    },
    onError: (error) => {
      console.error("Failed to configure external OAuth:", error);
      toast.error(
        error instanceof Error ? error.message : "Failed to configure OAuth",
      );
    },
  });

  const addOAuthProxyMutation = useAddOAuthProxyServerMutation({
    onSuccess: () => {
      invalidateAllToolset(queryClient);
      invalidateAllGetMcpMetadata(queryClient);
      invalidateAllListEnvironments(queryClient);
      telemetry.capture("mcp_event", {
        action: "oauth_proxy_configured",
        slug: toolsetSlug,
      });
      onSuccess();
    },
    onError: (error) => {
      console.error("Failed to configure OAuth proxy:", error);
      toast.error(
        error instanceof Error
          ? error.message
          : "Failed to configure OAuth proxy",
      );
    },
  });

  const updateOAuthProxyMutation = useUpdateOAuthProxyServerMutation({
    onSuccess: () => {
      invalidateAllToolset(queryClient);
      telemetry.capture("mcp_event", {
        action: "oauth_proxy_updated",
        slug: toolsetSlug,
      });
      onSuccess();
    },
    onError: (error) => {
      console.error("Failed to update OAuth proxy:", error);
      toast.error(
        error instanceof Error ? error.message : "Failed to update OAuth proxy",
      );
    },
  });

  const createEnvironmentMutation = useCreateEnvironmentMutation();
  const updateEnvironmentMutation = useUpdateEnvironmentMutation();
  const { data: mcpMetadataData } = useGetMcpMetadata(
    { toolsetSlug },
    undefined,
    { throwOnError: false, retry: false },
  );
  const mcpMetadata = mcpMetadataData?.metadata;

  const { data: environmentsData } = useListEnvironments();
  const environments = environmentsData?.environments ?? [];

  const attachedEnvironmentName = useMemo(() => {
    if (!mcpMetadata?.defaultEnvironmentId) return null;
    return (
      environments.find((e) => e.id === mcpMetadata.defaultEnvironmentId)
        ?.name ?? null
    );
  }, [environments, mcpMetadata?.defaultEnvironmentId]);

  const routes = useRoutes();

  // --- Submit handlers ---

  const handleExternalSubmit = useCallback(() => {
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
  }, [state, toolsetSlug, addExternalOAuthMutation]);

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
  }, [state]);

  const handleProxyFormNext = useCallback(() => {
    if (!validateProxyForm()) return;
    dispatch({ type: "PROXY_NEXT" });
  }, [validateProxyForm]);

  const handleProxyEditSubmit = useCallback(() => {
    if (state.step !== "oauth_proxy_server_metadata_form") return;
    if (!validateProxyForm()) return;

    const scopesArray = state.scopes
      .split(",")
      .map((s) => s.trim())
      .filter((s) => s.length > 0);

    const audienceChanged =
      state.audience !== proxyAudiencePrefilledRef.current;

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
  }, [state, toolsetSlug, updateOAuthProxyMutation, validateProxyForm]);

  const handleProxyCreateSubmit = useCallback(() => {
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
    let envName = `${toolset.name} OAuth`;
    if (existingNames.has(envName)) {
      let suffix = 1;
      while (existingNames.has(`${toolset.name} OAuth ${suffix}`)) {
        suffix++;
      }
      envName = `${toolset.name} OAuth ${suffix}`;
    }
    createEnvironmentMutation.mutate(
      {
        request: {
          createEnvironmentForm: {
            name: envName,
            organizationId: session.activeOrganizationId,
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
                toast.error("Failed to store OAuth credentials");
              },
            },
          );
        },
        onError: (error) => {
          console.error("Failed to create environment:", error);
          toast.error(
            error instanceof Error
              ? error.message
              : "Failed to create environment for OAuth credentials",
          );
        },
      },
    );
  }, [
    state,
    toolset.name,
    toolsetSlug,
    session.activeOrganizationId,
    environments,
    createEnvironmentMutation,
    updateEnvironmentMutation,
    addOAuthProxyMutation,
  ]);

  // --- Title ---

  const wizardTitle = editMode
    ? "Edit OAuth Proxy"
    : state.step === "path_selection"
      ? "Connect OAuth"
      : state.step === "oauth_proxy_client_credentials_form"
        ? "OAuth Client Credentials"
        : state.step === "external_oauth_server_metadata_form"
          ? "Configure External OAuth"
          : "Configure OAuth Proxy";

  const isProxySubmitting =
    createEnvironmentMutation.isPending ||
    updateEnvironmentMutation.isPending ||
    addOAuthProxyMutation.isPending;

  return (
    <Dialog open={isOpen} onOpenChange={onClose}>
      <Dialog.Content className="max-h-[90vh] max-w-6xl overflow-hidden">
        <Dialog.Header>
          <Dialog.Title>{wizardTitle}</Dialog.Title>
        </Dialog.Header>

        {state.step === "path_selection" && (
          <PathSelection
            discoveredOAuth={discoveredOAuth}
            dispatch={dispatch}
          />
        )}

        {state.step === "external_oauth_server_metadata_form" && (
          <ExternalOAuthForm
            state={state}
            dispatch={dispatch}
            discoveredOAuth={discoveredOAuth}
            hasMultipleOAuth2AuthCode={hasMultipleOAuth2AuthCode}
            oauth2SecurityCount={
              toolset.oauthEnablementMetadata?.oauth2SecurityCount
            }
            isPending={addExternalOAuthMutation.isPending}
            onSubmit={handleExternalSubmit}
          />
        )}

        {state.step === "oauth_proxy_server_metadata_form" && (
          <ProxyMetadataForm
            state={state}
            dispatch={dispatch}
            discoveredOAuth={discoveredOAuth}
            editMode={!!editMode}
            isEditPending={updateOAuthProxyMutation.isPending}
            onNext={handleProxyFormNext}
            onEditSubmit={handleProxyEditSubmit}
            onClose={onClose}
          />
        )}

        {state.step === "oauth_proxy_client_credentials_form" && (
          <ProxyCredentialsForm
            state={state}
            dispatch={dispatch}
            isSubmitting={isProxySubmitting}
            onSubmit={handleProxyCreateSubmit}
            attachedEnvironmentName={attachedEnvironmentName}
            environmentsLink={
              <routes.environments.Link className="text-muted-foreground hover:text-foreground text-sm transition-colors">
                Manage environments →
              </routes.environments.Link>
            }
          />
        )}
      </Dialog.Content>
    </Dialog>
  );
}

// ---------------------------------------------------------------------------
// Public wrapper (handles free-tier gating)
// ---------------------------------------------------------------------------

export function ConnectOAuthModal({
  isOpen,
  onClose,
  toolsetSlug,
  toolset,
  editMode,
}: {
  isOpen: boolean;
  onClose: () => void;
  toolsetSlug: string;
  toolset: Toolset;
  editMode?: { proxyServer: NonNullable<Toolset["oauthProxyServer"]> };
}) {
  const productTier = useProductTier();
  const queryClient = useQueryClient();
  const isAccountUpgrade = productTier.includes("base");

  if (isAccountUpgrade) {
    return (
      <FeatureRequestModal
        isOpen={isOpen}
        onClose={onClose}
        title="Connect OAuth"
        description="A Managed OAuth integration requires upgrading to a pro account type. Someone should be in touch shortly, or feel free to book a meeting directly."
        actionType="mcp_oauth_integration"
        icon={Globe}
        telemetryData={{ slug: toolsetSlug }}
        accountUpgrade={isAccountUpgrade}
      />
    );
  }

  return (
    <OAuthWizard
      isOpen={isOpen}
      onClose={onClose}
      toolsetSlug={toolsetSlug}
      toolset={toolset}
      editMode={editMode}
      onSuccess={() => {
        invalidateAllToolset(queryClient);
        toast.success(
          editMode
            ? "OAuth proxy server updated successfully"
            : "External OAuth server configured successfully",
        );
        onClose();
      }}
    />
  );
}
