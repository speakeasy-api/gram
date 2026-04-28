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
  useGramContext,
  useListEnvironments,
} from "@gram/client/react-query";
import { useQueryClient } from "@tanstack/react-query";
import { useMachine } from "@xstate/react";
import { Globe } from "lucide-react";
import { useEffect, useMemo, useState } from "react";

import { ExternalOAuthForm } from "./ExternalOAuthForm";
import { FatalErrorStep } from "./FatalErrorStep";
import { oauthWizardMachine, type WizardSnapshot } from "./machine";
import type { DiscoveredOAuth, Input, ProxyDefaults } from "./machine-types";
import { PathSelection } from "./PathSelection";
import { ProxyCredentialsForm } from "./ProxyCredentialsForm";
import { ProxyMetadataForm } from "./ProxyMetadataForm";
import { ResultStep } from "./ResultStep";
import { createWizardServices } from "./services";

// ---------------------------------------------------------------------------
// Container
// ---------------------------------------------------------------------------

type EditMode = { proxyServer: NonNullable<Toolset["oauthProxyServer"]> };

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
  editMode?: EditMode;
}) {
  // Force the inner machine to remount after the modal close animation
  // finishes (200ms). This replaces the old `dispatch RESET` pattern: it
  // resets all wizard state without flashing the path-selection step
  // mid-animation, and re-derives input from props on next open.
  const [resetKey, setResetKey] = useState(0);
  useEffect(() => {
    if (isOpen) return;
    const id = setTimeout(() => setResetKey((k) => k + 1), 200);
    return () => clearTimeout(id);
  }, [isOpen]);

  return (
    <Dialog open={isOpen} onOpenChange={onClose}>
      <Dialog.Content className="max-h-[90vh] max-w-6xl overflow-hidden">
        <WizardBody
          key={resetKey}
          onClose={onClose}
          toolsetSlug={toolsetSlug}
          toolset={toolset}
          editMode={editMode}
        />
      </Dialog.Content>
    </Dialog>
  );
}

// ---------------------------------------------------------------------------
// WizardBody — owns the machine instance. Remounted on close-and-reopen via
// the resetKey above so each new modal session starts fresh from input.
// ---------------------------------------------------------------------------

function WizardBody({
  onClose,
  toolsetSlug,
  toolset,
  editMode,
}: {
  onClose: () => void;
  toolsetSlug: string;
  toolset: Toolset;
  editMode?: EditMode;
}) {
  const client = useGramContext();
  const queryClient = useQueryClient();
  const telemetry = useTelemetry();
  const session = useSession();
  const { data: environmentsData } = useListEnvironments();

  const discovered = useDiscoveredOAuth(toolset);
  const externalDiscovered = discovered?.version === "2.1" ? discovered : null;
  const hasMultipleOAuth2AuthCode =
    toolset.oauthEnablementMetadata?.oauth2SecurityCount > 1;

  const existingEnvNames = useMemo(
    () => (environmentsData?.environments ?? []).map((e) => e.name),
    [environmentsData],
  );

  const editProxyDefaults = useMemo<ProxyDefaults | null>(() => {
    const proxy = editMode?.proxyServer;
    if (!proxy) return null;
    const provider = proxy.oauthProxyProviders?.[0];
    return {
      slug: proxy.slug ?? "",
      audience: proxy.audience ?? "",
      authorizationEndpoint: provider?.authorizationEndpoint ?? "",
      tokenEndpoint: provider?.tokenEndpoint ?? "",
      scopes: (provider?.scopesSupported ?? []).join(", "),
      tokenAuthMethod:
        provider?.tokenEndpointAuthMethodsSupported?.[0] ??
        "client_secret_post",
      environmentSlug: provider?.environmentSlug ?? "",
    };
  }, [editMode]);

  const provided = useMemo(
    () =>
      oauthWizardMachine.provide({
        actors: createWizardServices(client),
        actions: {
          invalidateOnExternalSuccess: () => invalidateAllToolset(queryClient),
          invalidateOnProxyCreate: () => {
            invalidateAllToolset(queryClient);
            invalidateAllGetMcpMetadata(queryClient);
            invalidateAllListEnvironments(queryClient);
          },
          invalidateOnProxyUpdate: () => invalidateAllToolset(queryClient),
          captureExternalSuccess: () =>
            telemetry.capture("mcp_event", {
              action: "external_oauth_configured",
              slug: toolsetSlug,
            }),
          captureProxyCreateSuccess: () =>
            telemetry.capture("mcp_event", {
              action: "oauth_proxy_configured",
              slug: toolsetSlug,
            }),
          captureProxyUpdateSuccess: () =>
            telemetry.capture("mcp_event", {
              action: "oauth_proxy_updated",
              slug: toolsetSlug,
            }),
        },
      }),
    [client, queryClient, telemetry, toolsetSlug],
  );

  const input: Input = {
    mode: editMode ? "edit" : "create",
    discovered,
    toolsetSlug,
    toolsetName: toolset.name,
    activeOrganizationId: session.activeOrganizationId,
    existingEnvNames,
    editProxyDefaults,
  };

  const [state, send] = useMachine(provided, { input });
  const ctx = state.context;

  const isProxyCreating =
    state.matches({ proxy: "creatingEnvironment" }) ||
    state.matches({ proxy: "creatingProxy" }) ||
    state.matches({ proxy: "rollingBackEnv" });

  return (
    <>
      <Dialog.Header>
        <Dialog.Title>{wizardTitle(state, !!editMode)}</Dialog.Title>
      </Dialog.Header>

      {state.matches("pathSelection") && (
        <PathSelection discovered={discovered} send={send} />
      )}

      {state.matches("external") && (
        <ExternalOAuthForm
          external={ctx.external}
          submitting={state.matches({ external: "submitting" })}
          discovered={externalDiscovered}
          hasMultipleOAuth2AuthCode={hasMultipleOAuth2AuthCode}
          oauth2SecurityCount={
            toolset.oauthEnablementMetadata?.oauth2SecurityCount ?? 0
          }
          send={send}
        />
      )}

      {(state.matches({ proxy: "metadata" }) ||
        state.matches({ proxy: "updating" })) && (
        <ProxyMetadataForm
          proxy={ctx.proxy}
          error={ctx.error}
          editPending={state.matches({ proxy: "updating" })}
          discovered={discovered}
          editMode={!!editMode}
          send={send}
          onClose={onClose}
        />
      )}

      {(state.matches({ proxy: "credentials" }) || isProxyCreating) && (
        <ProxyCredentialsForm
          proxy={ctx.proxy}
          error={ctx.error}
          submitting={isProxyCreating}
          send={send}
        />
      )}

      {state.matches({ proxy: "fatalError" }) && (
        <FatalErrorStep error={ctx.error} onClose={onClose} />
      )}

      {state.matches("result") && ctx.result && (
        <ResultStep message={ctx.result.message} onClose={onClose} />
      )}
    </>
  );
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function wizardTitle(state: WizardSnapshot, editMode: boolean): string {
  if (state.matches("pathSelection")) return "Connect OAuth";
  if (state.matches("external")) return "Configure External OAuth";
  if (
    state.matches({ proxy: "metadata" }) ||
    state.matches({ proxy: "updating" })
  )
    return editMode ? "Edit OAuth Proxy" : "Configure OAuth Proxy";
  if (
    state.matches({ proxy: "credentials" }) ||
    state.matches({ proxy: "creatingEnvironment" }) ||
    state.matches({ proxy: "creatingProxy" }) ||
    state.matches({ proxy: "rollingBackEnv" })
  )
    return "OAuth Client Credentials";
  if (state.matches({ proxy: "fatalError" })) return "Configuration Failed";
  if (state.matches("result")) return "OAuth Configured";
  return "Connect OAuth";
}

function useDiscoveredOAuth(toolset: Toolset): DiscoveredOAuth | null {
  return useMemo<DiscoveredOAuth | null>(() => {
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
  editMode?: EditMode;
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
