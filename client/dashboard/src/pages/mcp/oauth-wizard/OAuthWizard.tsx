import { FeatureRequestModal } from "@/components/FeatureRequestModal";
import { Dialog } from "@/components/ui/dialog";
import { useSession } from "@/contexts/Auth";
import { useTelemetry } from "@/contexts/Telemetry";
import { Toolset } from "@/lib/toolTypes";
import { getServerURL } from "@/lib/utils";
import { useProductTier } from "@/hooks/useProductTier";
import {
  invalidateAllGetMcpMetadata,
  invalidateAllListEnvironments,
  invalidateAllToolset,
  useAddExternalOAuthServerMutation,
  useAddOAuthProxyServerMutation,
  useUpdateOAuthProxyServerMutation,
  useCreateEnvironmentMutation,
  useListEnvironments,
} from "@gram/client/react-query";
import { useQueryClient } from "@tanstack/react-query";
import { Globe } from "lucide-react";
import { useEffect, useMemo, useReducer, useRef } from "react";

import { useStepActions } from "./actions";
import { ExternalOAuthForm } from "./ExternalOAuthForm";
import { PathSelection } from "./PathSelection";
import { ProxyCredentialsForm } from "./ProxyCredentialsForm";
import { ProxyMetadataForm } from "./ProxyMetadataForm";
import { ResultStep } from "./ResultStep";
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
  editMode,
}: {
  isOpen: boolean;
  onClose: () => void;
  toolsetSlug: string;
  toolset: Toolset;
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
      title: "Edit OAuth Proxy",
      defaults: {
        slug: editProxyServer.slug ?? "",
        audience: initialAudience,
        authorizationEndpoint: provider?.authorizationEndpoint ?? "",
        tokenEndpoint: provider?.tokenEndpoint ?? "",
        scopes: (provider?.scopesSupported ?? []).join(", "),
        tokenAuthMethod:
          provider?.tokenEndpointAuthMethodsSupported?.[0] ??
          "client_secret_basic",
        environmentSlug: provider?.environmentSlug ?? "",
      },
    });
  }, [editProxyServer]);

  // Reset wizard state after the modal close animation finishes.
  useEffect(() => {
    if (isOpen) return;
    const id = setTimeout(() => dispatch({ type: "RESET" }), 200);
    return () => clearTimeout(id);
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
      dispatch({
        type: "SET_RESULT",
        success: true,
        message: "Your external OAuth server has been configured successfully.",
      });
    },
    onError: (error) => {
      console.error("Failed to configure external OAuth:", error);
      dispatch({
        type: "SET_RESULT",
        success: false,
        message:
          error instanceof Error ? error.message : "Failed to configure OAuth",
      });
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
      dispatch({
        type: "SET_RESULT",
        success: true,
        message:
          "Your OAuth proxy has been configured successfully. Client credentials have been stored in a new environment.",
      });
    },
    onError: (error) => {
      console.error("Failed to configure OAuth proxy:", error);
      dispatch({
        type: "SET_RESULT",
        success: false,
        message:
          error instanceof Error
            ? error.message
            : "Failed to configure OAuth proxy",
      });
    },
  });

  const updateOAuthProxyMutation = useUpdateOAuthProxyServerMutation({
    onSuccess: () => {
      invalidateAllToolset(queryClient);
      telemetry.capture("mcp_event", {
        action: "oauth_proxy_updated",
        slug: toolsetSlug,
      });
      dispatch({
        type: "SET_RESULT",
        success: true,
        message: "Your OAuth proxy server has been updated successfully.",
      });
    },
    onError: (error) => {
      console.error("Failed to update OAuth proxy:", error);
      dispatch({
        type: "SET_RESULT",
        success: false,
        message:
          error instanceof Error
            ? error.message
            : "Failed to update OAuth proxy",
      });
    },
  });

  const createEnvironmentMutation = useCreateEnvironmentMutation();
  const { data: environmentsData } = useListEnvironments();
  const environments = environmentsData?.environments ?? [];

  // --- Step actions ---

  const actions = useStepActions({
    state,
    dispatch,
    toolsetSlug,
    toolsetName: toolset.name,
    activeOrganizationId: session.activeOrganizationId,
    environments,
    proxyAudiencePrefilled: proxyAudiencePrefilledRef.current,
    discoveredOAuth,
    addExternalOAuthMutation,
    addOAuthProxyMutation,
    updateOAuthProxyMutation,
    createEnvironmentMutation,
  });

  const wizardTitle = state.title;

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
            discoveredOAuth={
              discoveredOAuth?.version === "2.1" ? discoveredOAuth : null
            }
            hasMultipleOAuth2AuthCode={hasMultipleOAuth2AuthCode}
            oauth2SecurityCount={
              toolset.oauthEnablementMetadata?.oauth2SecurityCount
            }
            isPending={addExternalOAuthMutation.isPending}
            onSubmit={actions.external_oauth_server_metadata_form.submit}
          />
        )}

        {state.step === "oauth_proxy_server_metadata_form" && (
          <ProxyMetadataForm
            state={state}
            dispatch={dispatch}
            discoveredOAuth={discoveredOAuth}
            editMode={!!editMode}
            isEditPending={updateOAuthProxyMutation.isPending}
            isNextPending={
              actions.oauth_proxy_server_metadata_form.isNextPending
            }
            onNext={actions.oauth_proxy_server_metadata_form.next}
            onEditSubmit={actions.oauth_proxy_server_metadata_form.editSubmit}
            onClose={onClose}
          />
        )}

        {state.step === "oauth_proxy_client_credentials_form" && (
          <ProxyCredentialsForm
            state={state}
            dispatch={dispatch}
            isSubmitting={
              actions.oauth_proxy_client_credentials_form.isSubmitting
            }
            onSubmit={actions.oauth_proxy_client_credentials_form.submit}
          />
        )}

        {state.step === "result" && (
          <ResultStep state={state} onClose={onClose} />
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
    />
  );
}
