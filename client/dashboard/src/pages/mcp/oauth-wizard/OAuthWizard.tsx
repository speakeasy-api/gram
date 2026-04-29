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
} from "@gram/client/react-query";
import { useQueryClient } from "@tanstack/react-query";
import { Globe } from "lucide-react";
import { useEffect, useMemo, useState } from "react";

import { ExternalOAuthForm } from "./ExternalOAuthForm";
import { FatalErrorStep } from "./FatalErrorStep";
import {
  oauthWizardMachine,
  selectWizardTitle,
  WizardContext,
} from "./machine";
import type { DiscoveredOAuth, Input } from "./machine-types";
import { PathSelection } from "./PathSelection";
import { ProxyCredentialsForm } from "./ProxyCredentialsForm";
import { ProxyMetadataForm } from "./ProxyMetadataForm";
import { ResultStep } from "./ResultStep";
import { createWizardServices } from "./services";

// ---------------------------------------------------------------------------
// Container
// ---------------------------------------------------------------------------

function OAuthWizard({
  isOpen,
  onClose,
  toolsetSlug,
  toolset,
}: {
  isOpen: boolean;
  onClose: () => void;
  toolsetSlug: string;
  toolset: Toolset;
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
}: {
  onClose: () => void;
  toolsetSlug: string;
  toolset: Toolset;
}) {
  const client = useGramContext();
  const queryClient = useQueryClient();
  const telemetry = useTelemetry();
  const session = useSession();

  const discovered = useDiscoveredOAuth(toolset);

  const provided = useMemo(
    () =>
      oauthWizardMachine.provide({
        actors: createWizardServices(client, queryClient),
        actions: {
          invalidateOnExternalSuccess: () => invalidateAllToolset(queryClient),
          invalidateOnProxyCreate: () => {
            invalidateAllToolset(queryClient);
            invalidateAllGetMcpMetadata(queryClient);
            invalidateAllListEnvironments(queryClient);
          },
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
        },
      }),
    [client, queryClient, telemetry, toolsetSlug],
  );

  const input: Input = {
    discovered,
    toolsetSlug,
    toolsetName: toolset.name,
    activeOrganizationId: session.activeOrganizationId,
  };

  return (
    <WizardContext.Provider logic={provided} options={{ input }}>
      <WizardSteps onClose={onClose} toolset={toolset} />
    </WizardContext.Provider>
  );
}

function WizardSteps({
  onClose,
  toolset,
}: {
  onClose: () => void;
  toolset: Toolset;
}) {
  const state = WizardContext.useSelector((s) => s);
  const oauth2SecurityCount =
    toolset.oauthEnablementMetadata?.oauth2SecurityCount ?? 0;
  const hasMultipleOAuth2AuthCode = oauth2SecurityCount > 1;

  const isProxyCreating = state.matches({ proxy: "submitting" });

  return (
    <>
      <Dialog.Header>
        <Dialog.Title>{selectWizardTitle(state)}</Dialog.Title>
      </Dialog.Header>

      {state.matches("pathSelection") && <PathSelection />}

      {state.matches("external") && (
        <ExternalOAuthForm
          hasMultipleOAuth2AuthCode={hasMultipleOAuth2AuthCode}
          oauth2SecurityCount={oauth2SecurityCount}
        />
      )}

      {state.matches({ proxy: "metadata" }) && <ProxyMetadataForm />}

      {(state.matches({ proxy: "credentials" }) || isProxyCreating) && (
        <ProxyCredentialsForm />
      )}

      {state.matches({ proxy: "fatalError" }) && (
        <FatalErrorStep error={state.context.error} onClose={onClose} />
      )}

      {state.matches("result") && state.context.result && (
        <ResultStep message={state.context.result.message} onClose={onClose} />
      )}
    </>
  );
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

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
}: {
  isOpen: boolean;
  onClose: () => void;
  toolsetSlug: string;
  toolset: Toolset;
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
    />
  );
}
