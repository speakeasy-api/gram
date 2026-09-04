import { FeatureRequestModal } from "@/components/FeatureRequestModal";
import { Alert, AlertDescription } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useSession } from "@/contexts/Auth";
import { useFetcher } from "@/contexts/Fetcher";
import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { Toolset } from "@/lib/toolTypes";
import { getServerURL } from "@/lib/utils";
import { useProductTier } from "@/hooks/useProductTier";
import type { RemoteSessionIssuerDraft } from "@gram/client/models/components/remotesessionissuerdraft.js";
import { buildFetchRemoteSessionIssuerMetadataMutation } from "@gram/client/react-query/fetchRemoteSessionIssuerMetadata.js";
import { invalidateAllGetMcpMetadata } from "@gram/client/react-query/getMcpMetadata.js";
import { invalidateAllListEnvironments } from "@gram/client/react-query/listEnvironments.js";
import { invalidateAllToolset } from "@gram/client/react-query/toolset.js";
import { buildUpdateExternalOAuthServerMutation } from "@gram/client/react-query/updateExternalOAuthServer.js";
import { useQueryClient } from "@tanstack/react-query";
import { Globe } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";

import { AutoConfigureLoader } from "./AutoConfigureLoader";
import { AutoRegisterFailedStep } from "./AutoRegisterFailedStep";
import { ExternalOAuthForm } from "./ExternalOAuthForm";
import { validateProviderIssuerUrl } from "./externalOAuthMetadata";
import { FatalErrorStep } from "./FatalErrorStep";
import {
  oauthWizardMachine,
  selectWizardTitle,
  WizardContext,
} from "./machine";
import type { DiscoveredOAuth, Input as WizardInput } from "./machine-types";
import { PathSelection } from "./PathSelection";
import { ProxyCredentialsForm } from "./ProxyCredentialsForm";
import { ProxyMetadataForm } from "./ProxyMetadataForm";
import { ResultStep } from "./ResultStep";
import { createWizardServices } from "./services";

// ---------------------------------------------------------------------------
// Container
// ---------------------------------------------------------------------------

export type ExistingExternalOAuthConfig = {
  issuer: string;
  metadata?: Record<string, unknown>;
  providerHosted?: boolean;
};

function OAuthWizard({
  isOpen,
  onClose,
  toolsetSlug,
  toolset,
  initialPath,
  existingConfig,
}: {
  isOpen: boolean;
  onClose: () => void;
  toolsetSlug: string;
  toolset: Toolset;
  initialPath?: WizardInput["initialPath"];
  existingConfig?: ExistingExternalOAuthConfig;
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
        {existingConfig ? (
          <ExistingConfigReview
            key={resetKey}
            config={existingConfig}
            isOpen={isOpen}
            onClose={onClose}
            toolsetSlug={toolsetSlug}
          />
        ) : (
          <WizardBody
            key={resetKey}
            onClose={onClose}
            toolsetSlug={toolsetSlug}
            toolset={toolset}
            initialPath={initialPath}
          />
        )}
      </Dialog.Content>
    </Dialog>
  );
}

function ExistingConfigReview({
  config,
  isOpen,
  onClose,
  toolsetSlug,
}: {
  config: ExistingExternalOAuthConfig;
  isOpen: boolean;
  onClose: () => void;
  toolsetSlug: string;
}) {
  const client = useSdkClient();
  const queryClient = useQueryClient();
  const [stage, setStage] = useState<"intro" | "review" | "clear" | "success">(
    "intro",
  );
  const [issuer, setIssuer] = useState(config.issuer);
  const [verified, setVerified] = useState<RemoteSessionIssuerDraft | null>(
    null,
  );
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);
  const requestRef = useRef<{
    generation: number;
    controller?: AbortController;
  }>({
    generation: 0,
  });

  useEffect(() => {
    if (isOpen) return;
    requestRef.current.controller?.abort();
    requestRef.current = { generation: requestRef.current.generation + 1 };
    setStage("intro");
    setIssuer(config.issuer);
    setVerified(null);
    setError(null);
    setPending(false);
  }, [config.issuer, isOpen]);

  useEffect(
    () => () => {
      requestRef.current.controller?.abort();
    },
    [],
  );

  const review = async () => {
    const validationError = validateProviderIssuerUrl(issuer);
    if (validationError) {
      setError(validationError);
      return;
    }

    requestRef.current.controller?.abort();
    const controller = new AbortController();
    const generation = requestRef.current.generation + 1;
    requestRef.current = { generation, controller };
    setPending(true);
    setError(null);
    try {
      const result = await buildFetchRemoteSessionIssuerMetadataMutation(
        client,
      ).mutationFn({
        request: { fetchIssuerMetadataRequestBody: { issuer: issuer.trim() } },
        options: { fetchOptions: { signal: controller.signal } },
      });
      if (requestRef.current.generation !== generation) return;
      setVerified(result);
      setStage("review");
    } catch (err) {
      if (requestRef.current.generation !== generation) return;
      setError(
        err instanceof Error
          ? err.message
          : "Could not verify provider metadata",
      );
    } finally {
      if (requestRef.current.generation === generation) setPending(false);
    }
  };

  const update = async (providerHosted: boolean) => {
    if (!providerHosted && !config.metadata) return;
    setPending(true);
    setError(null);
    try {
      await buildUpdateExternalOAuthServerMutation(client).mutationFn({
        request: {
          slug: toolsetSlug,
          updateExternalOAuthServerRequestBody: providerHosted
            ? { authorizationServerIssuer: verified?.issuer }
            : { metadata: config.metadata },
        },
      });
      await Promise.all([
        invalidateAllToolset(queryClient),
        invalidateAllGetMcpMetadata(queryClient),
      ]);
      setStage("success");
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Could not update OAuth metadata",
      );
    } finally {
      setPending(false);
    }
  };

  return (
    <>
      <Dialog.Header>
        <Dialog.Title>Review OAuth metadata</Dialog.Title>
      </Dialog.Header>
      <div className="px-6 py-4">
        <Stack gap={4}>
          <Text>
            Gram always hosts protected-resource metadata for this MCP server.
          </Text>
          <Text muted small>
            Switching authorization-server metadata keeps existing tokens and
            registrations. Some clients may ask users to authenticate again.
          </Text>
          {stage === "intro" && (
            <Stack gap={2}>
              <Label htmlFor="existing-oauth-issuer">Issuer URL</Label>
              <Input
                id="existing-oauth-issuer"
                value={issuer}
                onChange={(value) => {
                  setIssuer(value);
                  setVerified(null);
                  setError(null);
                }}
                validate={(value) => validateProviderIssuerUrl(value) ?? true}
              />
            </Stack>
          )}
          {stage === "review" && verified && (
            <Stack gap={2}>
              <Text small>Issuer: {verified.issuer}</Text>
              <Text small>
                Authorization endpoint:{" "}
                {verified.authorizationEndpoint ?? "Not advertised"}
              </Text>
              <Text small>
                Token endpoint: {verified.tokenEndpoint ?? "Not advertised"}
              </Text>
              <Text small>
                RFC 9207 support:{" "}
                {verified.authorizationResponseIssParameterSupported
                  ? "Supported"
                  : "Not advertised"}
              </Text>
              {!!verified.discoveryWarnings?.length && (
                <Alert variant="warning">
                  <AlertDescription>
                    {verified.discoveryWarnings.join(" ")}
                  </AlertDescription>
                </Alert>
              )}
            </Stack>
          )}
          {stage === "clear" && (
            <Text>
              Confirm that Gram should continue hosting authorization-server
              metadata.
            </Text>
          )}
          {stage === "success" && <Text>OAuth metadata updated.</Text>}
          {error && (
            <Alert variant="error">
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          )}
        </Stack>
      </div>
      <Dialog.Footer className="flex justify-between">
        {stage === "success" ? (
          <Button onClick={onClose}>Done</Button>
        ) : (
          <>
            <Button variant="secondary" onClick={onClose}>
              Cancel
            </Button>
            <div className="flex gap-2">
              {stage === "intro" && (
                <>
                  {config.metadata && (
                    <Button
                      variant="secondary"
                      onClick={() => setStage("clear")}
                    >
                      {config.providerHosted
                        ? "Use Gram-hosted metadata"
                        : "Keep Gram-hosted metadata"}
                    </Button>
                  )}
                  <Button onClick={() => void review()} disabled={pending}>
                    {pending ? "Reviewing..." : "Review update"}
                  </Button>
                </>
              )}
              {stage === "review" && (
                <Button onClick={() => void update(true)} disabled={pending}>
                  {pending ? "Updating..." : "Use provider-hosted metadata"}
                </Button>
              )}
              {stage === "clear" && (
                <Button onClick={() => void update(false)} disabled={pending}>
                  {pending ? "Updating..." : "Confirm Gram-hosted metadata"}
                </Button>
              )}
            </div>
          </>
        )}
      </Dialog.Footer>
    </>
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
  initialPath,
}: {
  onClose: () => void;
  toolsetSlug: string;
  toolset: Toolset;
  initialPath?: WizardInput["initialPath"];
}) {
  const client = useSdkClient();
  const queryClient = useQueryClient();
  const telemetry = useTelemetry();
  const session = useSession();
  const { fetch: authedFetch } = useFetcher();

  const discovered = useDiscoveredOAuth(toolset);

  const provided = useMemo(
    () =>
      oauthWizardMachine.provide({
        actors: createWizardServices(client, authedFetch),
        actions: {
          invalidateOnExternalSuccess: () => {
            void invalidateAllToolset(queryClient);
          },
          invalidateOnProxyCreate: () => {
            void invalidateAllToolset(queryClient);
            void invalidateAllGetMcpMetadata(queryClient);
            void invalidateAllListEnvironments(queryClient);
          },
          captureExternalSuccess: () => {
            void telemetry.capture("mcp_event", {
              action: "external_oauth_configured",
              slug: toolsetSlug,
            });
          },
          captureProxyCreateSuccess: () => {
            void telemetry.capture("mcp_event", {
              action: "oauth_proxy_configured",
              slug: toolsetSlug,
            });
          },
        },
      }),
    [client, queryClient, telemetry, toolsetSlug, authedFetch],
  );

  const input: WizardInput = {
    discovered,
    initialPath,
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
  const isAutoRegistering = state.context.autoRegistering;

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
          onCancel={onClose}
        />
      )}

      {state.matches({ proxy: "metadata" }) && <ProxyMetadataForm />}

      {(state.matches({ proxy: "registering" }) ||
        (isProxyCreating && isAutoRegistering)) && <AutoConfigureLoader />}

      {state.matches({ proxy: "autoRegisterFailed" }) && (
        <AutoRegisterFailedStep error={state.context.error} onClose={onClose} />
      )}

      {(state.matches({ proxy: "credentials" }) ||
        (isProxyCreating && !isAutoRegistering)) && <ProxyCredentialsForm />}

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
  initialPath,
  existingConfig,
}: {
  isOpen: boolean;
  onClose: () => void;
  toolsetSlug: string;
  toolset: Toolset;
  initialPath?: WizardInput["initialPath"];
  existingConfig?: ExistingExternalOAuthConfig;
}): JSX.Element {
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
      initialPath={initialPath}
      existingConfig={existingConfig}
    />
  );
}
