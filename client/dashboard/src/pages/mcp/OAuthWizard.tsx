import { FeatureRequestModal } from "@/components/FeatureRequestModal";
import { Dialog } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Link } from "@/components/ui/link";
import { TextArea } from "@/components/ui/textarea";
import { Type } from "@/components/ui/type";
import { useSession } from "@/contexts/Auth";
import { useTelemetry } from "@/contexts/Telemetry";
import { Toolset } from "@/lib/toolTypes";
import { cn, getServerURL } from "@/lib/utils";
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
  useMcpMetadataSetMutation,
  useUpdateEnvironmentMutation,
} from "@gram/client/react-query";
import { Button, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, Globe, LockIcon } from "lucide-react";
import React, {
  useCallback,
  useEffect,
  useMemo,
  useReducer,
  useRef,
} from "react";
import { toast } from "sonner";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface DiscoveredOAuth {
  slug: string;
  name: string;
  version: string;
  metadata: Record<string, unknown>;
}

interface ProxyFormData {
  slug: string;
  authorizationEndpoint: string;
  tokenEndpoint: string;
  scopes: string;
  audience: string;
  tokenAuthMethod: string;
  environmentSlug: string;
}

type WizardState =
  | { step: "path_selection" }
  | {
      step: "external_oauth_server_metadata_form";
      slug: string;
      metadataJson: string;
      jsonError: string | null;
      prefilled: boolean;
    }
  | {
      step: "oauth_proxy_server_metadata_form";
      slug: string;
      authorizationEndpoint: string;
      tokenEndpoint: string;
      scopes: string;
      audience: string;
      tokenAuthMethod: string;
      environmentSlug: string;
      error: string | null;
      prefilled: boolean;
    }
  | {
      step: "oauth_proxy_client_credentials_form";
      proxyFormData: ProxyFormData;
      clientId: string;
      clientSecret: string;
      error: string | null;
    };

type WizardAction =
  | { type: "SELECT_EXTERNAL"; discoveredOAuth?: DiscoveredOAuth | null }
  | {
      type: "SELECT_PROXY";
      discoveredOAuth?: DiscoveredOAuth | null;
      defaults?: Partial<ProxyFormData>;
    }
  | { type: "BACK" }
  | { type: "PROXY_NEXT" }
  | { type: "UPDATE_FIELD"; field: string; value: string }
  | { type: "SET_ERROR"; error: string | null }
  | { type: "APPLY_DISCOVERED"; discoveredOAuth: DiscoveredOAuth }
  | { type: "RESET" };

// ---------------------------------------------------------------------------
// Reducer
// ---------------------------------------------------------------------------

const INITIAL_STATE: WizardState = { step: "path_selection" };

function applyExternalDiscovered(d: DiscoveredOAuth) {
  return {
    slug: d.slug,
    metadataJson: JSON.stringify(d.metadata, null, 2),
    jsonError: null,
    prefilled: true,
  };
}

function applyProxyDiscovered(d: DiscoveredOAuth): Partial<ProxyFormData> {
  const m = d.metadata;
  const partial: Partial<ProxyFormData> = { slug: d.slug };
  if (typeof m.authorization_endpoint === "string")
    partial.authorizationEndpoint = m.authorization_endpoint;
  if (typeof m.token_endpoint === "string")
    partial.tokenEndpoint = m.token_endpoint;
  if (Array.isArray(m.scopes_supported))
    partial.scopes = m.scopes_supported.join(", ");
  return partial;
}

function makeProxyState(
  overrides?: Partial<ProxyFormData> & { prefilled?: boolean },
): Extract<WizardState, { step: "oauth_proxy_server_metadata_form" }> {
  return {
    step: "oauth_proxy_server_metadata_form",
    slug: overrides?.slug ?? "",
    authorizationEndpoint: overrides?.authorizationEndpoint ?? "",
    tokenEndpoint: overrides?.tokenEndpoint ?? "",
    scopes: overrides?.scopes ?? "",
    audience: overrides?.audience ?? "",
    tokenAuthMethod: overrides?.tokenAuthMethod ?? "client_secret_post",
    environmentSlug: overrides?.environmentSlug ?? "",
    error: null,
    prefilled: overrides?.prefilled ?? false,
  };
}

function extractProxyFormData(
  s: Extract<WizardState, { step: "oauth_proxy_server_metadata_form" }>,
): ProxyFormData {
  return {
    slug: s.slug,
    authorizationEndpoint: s.authorizationEndpoint,
    tokenEndpoint: s.tokenEndpoint,
    scopes: s.scopes,
    audience: s.audience,
    tokenAuthMethod: s.tokenAuthMethod,
    environmentSlug: s.environmentSlug,
  };
}

function wizardReducer(state: WizardState, action: WizardAction): WizardState {
  switch (action.type) {
    case "SELECT_EXTERNAL": {
      const discovered = action.discoveredOAuth
        ? applyExternalDiscovered(action.discoveredOAuth)
        : {};
      return {
        step: "external_oauth_server_metadata_form",
        slug: "",
        metadataJson: "",
        jsonError: null,
        prefilled: false,
        ...discovered,
      };
    }

    case "SELECT_PROXY": {
      const discovered = action.discoveredOAuth
        ? { ...applyProxyDiscovered(action.discoveredOAuth), prefilled: true }
        : {};
      return makeProxyState({ ...action.defaults, ...discovered });
    }

    case "BACK": {
      if (state.step === "oauth_proxy_client_credentials_form") {
        return makeProxyState(state.proxyFormData);
      }
      return INITIAL_STATE;
    }

    case "PROXY_NEXT": {
      if (state.step !== "oauth_proxy_server_metadata_form") return state;
      return {
        step: "oauth_proxy_client_credentials_form",
        proxyFormData: extractProxyFormData(state),
        clientId: "",
        clientSecret: "",
        error: null,
      };
    }

    case "UPDATE_FIELD": {
      if (state.step === "path_selection") return state;
      return { ...state, [action.field]: action.value } as WizardState;
    }

    case "SET_ERROR": {
      if (state.step === "path_selection") return state;
      return { ...state, error: action.error } as WizardState;
    }

    case "APPLY_DISCOVERED": {
      if (state.step === "external_oauth_server_metadata_form") {
        return { ...state, ...applyExternalDiscovered(action.discoveredOAuth) };
      }
      if (state.step === "oauth_proxy_server_metadata_form") {
        return {
          ...state,
          ...applyProxyDiscovered(action.discoveredOAuth),
          prefilled: true,
        };
      }
      return state;
    }

    case "RESET":
      return INITIAL_STATE;

    default:
      return state;
  }
}

// ---------------------------------------------------------------------------
// Step components
// ---------------------------------------------------------------------------

function PathSelection({
  discoveredOAuth,
  dispatch,
}: {
  discoveredOAuth: DiscoveredOAuth | null;
  dispatch: React.Dispatch<WizardAction>;
}) {
  return (
    <div className="space-y-4">
      {discoveredOAuth && (
        <div className="border-border bg-muted/50 flex items-start justify-between gap-4 rounded-md border p-4">
          <div>
            <Type small className="font-medium">
              OAuth detected from {discoveredOAuth.name}
            </Type>
            <Type muted small className="mt-1">
              We discovered OAuth {discoveredOAuth.version} metadata from this
              server. The configuration will be pre-filled for either
              configuration below.
            </Type>
          </div>
        </div>
      )}

      <Type muted small>
        Choose how you want to configure OAuth for this MCP server.
      </Type>

      <div className="grid grid-cols-2 gap-4">
        <button
          type="button"
          className={cn(
            "border-border flex flex-col items-start gap-2 rounded-lg border p-6 text-left transition-colors",
            "hover:border-primary hover:bg-muted/50",
          )}
          onClick={() => dispatch({ type: "SELECT_EXTERNAL", discoveredOAuth })}
        >
          <Globe className="text-muted-foreground h-6 w-6" />
          <Type className="font-medium">External OAuth</Type>
          <Type muted small>
            For APIs that meet the MCP OAuth spec. Uses authorization code flow
            with your external authorization server.
          </Type>
        </button>

        <button
          type="button"
          className={cn(
            "border-border flex flex-col items-start gap-2 rounded-lg border p-6 text-left transition-colors",
            "hover:border-primary hover:bg-muted/50",
          )}
          onClick={() => dispatch({ type: "SELECT_PROXY", discoveredOAuth })}
        >
          <LockIcon className="text-muted-foreground h-6 w-6" />
          <Type className="font-medium">OAuth Proxy</Type>
          <Type muted small>
            For internal servers that don't natively support MCP OAuth. Gram
            proxies OAuth on behalf of your server.
          </Type>
        </button>
      </div>
    </div>
  );
}

function ExternalOAuthForm({
  state,
  dispatch,
  discoveredOAuth,
  hasMultipleOAuth2AuthCode,
  oauth2SecurityCount,
  isPending,
  onSubmit,
}: {
  state: Extract<WizardState, { step: "external_oauth_server_metadata_form" }>;
  dispatch: React.Dispatch<WizardAction>;
  discoveredOAuth: DiscoveredOAuth | null;
  hasMultipleOAuth2AuthCode: boolean;
  oauth2SecurityCount: number;
  isPending: boolean;
  onSubmit: () => void;
}) {
  return (
    <>
      <div className="max-h-[60vh] space-y-4 overflow-auto">
        {hasMultipleOAuth2AuthCode && (
          <div className="mb-4 rounded-md border border-red-200 bg-red-50 p-4">
            <Type small className="mt-1 text-red-600">
              Not Supported: This MCP server has {oauth2SecurityCount} OAuth2
              security schemes detected.
            </Type>
          </div>
        )}
        {discoveredOAuth && !state.prefilled && (
          <div className="border-border bg-muted/50 mb-4 flex items-start justify-between gap-4 rounded-md border p-4">
            <div>
              <Type small className="font-medium">
                OAuth detected from {discoveredOAuth.name}
              </Type>
              <Type muted small className="mt-1">
                We discovered OAuth {discoveredOAuth.version} metadata from this
                server. You can use it to pre-fill the form below.
              </Type>
            </div>
            <Button
              size="sm"
              variant="secondary"
              onClick={() =>
                dispatch({
                  type: "APPLY_DISCOVERED",
                  discoveredOAuth,
                })
              }
            >
              Apply
            </Button>
          </div>
        )}
        {state.prefilled && (
          <div className="border-border bg-muted/50 mb-4 rounded-md border p-4">
            <Type small className="font-medium">
              Pre-filled from detected OAuth metadata
            </Type>
            <Type muted small className="mt-1">
              This form has been pre-filled with information Speakeasy detected
              about this server's OAuth requirements. Please review carefully
              and refer to the MCP server or API's documentation to confirm
              these values are correct.
            </Type>
          </div>
        )}
        <div>
          <Type className="mb-2 font-medium">
            External OAuth Server Configuration
          </Type>
          <Type muted small className="mb-4">
            Configure your MCP server to use an external authorization server if
            your API fits the very specific MCP OAuth requirements.{" "}
            <Link
              external
              to="https://docs.getgram.ai/host-mcp/adding-oauth#authorization-code"
            >
              Docs
            </Link>
          </Type>

          <Stack gap={4}>
            <div>
              <Type className="mb-2 font-medium">OAuth Server Slug</Type>
              <Input
                placeholder="my-oauth-server"
                value={state.slug}
                onChange={(v: string) =>
                  dispatch({ type: "UPDATE_FIELD", field: "slug", value: v })
                }
                maxLength={40}
              />
            </div>

            <div>
              <Type className="mb-2 font-medium">
                OAuth Authorization Server Metadata
              </Type>
              {state.jsonError && (
                <Type className="mt-1 text-sm text-red-500!">
                  {state.jsonError}
                </Type>
              )}
              <TextArea
                placeholder={`{
  "issuer": "https://your-oauth-server.com",
  "authorization_endpoint": "https://your-oauth-server.com/oauth/authorize",
  "registration_endpoint": "https://your-oauth-server.com/oauth/register",
  "token_endpoint": "https://your-oauth-server.com/oauth/token",
  "scopes_supported": ["read", "write"],
  "response_types_supported": ["code"],
  "grant_types_supported": ["authorization_code"],
  "token_endpoint_auth_methods_supported": [
    "client_secret_post"
  ],
  "code_challenge_methods_supported": [
    "plain",
    "S256"
  ]
}`}
                value={state.metadataJson}
                onChange={(value: string) => {
                  dispatch({
                    type: "UPDATE_FIELD",
                    field: "metadataJson",
                    value,
                  });
                  if (state.jsonError) {
                    dispatch({ type: "SET_ERROR", error: null });
                  }
                }}
                rows={12}
                className="font-mono text-sm"
              />
            </div>
          </Stack>
        </div>
      </div>

      <Dialog.Footer className="flex justify-between">
        <Button variant="secondary" onClick={() => dispatch({ type: "BACK" })}>
          Back
        </Button>
        <div className="ml-auto">
          <Button
            onClick={onSubmit}
            disabled={
              hasMultipleOAuth2AuthCode ||
              isPending ||
              !state.slug.trim() ||
              !state.metadataJson.trim()
            }
          >
            {isPending ? "Configuring..." : "Configure External OAuth"}
          </Button>
        </div>
      </Dialog.Footer>
    </>
  );
}

function ProxyMetadataForm({
  state,
  dispatch,
  discoveredOAuth,
  editMode,
  isEditPending,
  onNext,
  onEditSubmit,
  onClose,
}: {
  state: Extract<WizardState, { step: "oauth_proxy_server_metadata_form" }>;
  dispatch: React.Dispatch<WizardAction>;
  discoveredOAuth: DiscoveredOAuth | null;
  editMode: boolean;
  isEditPending: boolean;
  onNext: () => void;
  onEditSubmit: () => void;
  onClose: () => void;
}) {
  return (
    <>
      <div className="max-h-[60vh] space-y-4 overflow-auto">
        <div>
          <Type muted small className="mb-2 font-medium">
            Ideal for internal MCP servers. The OAuth Proxy configuration can be
            used to set up auth for an MCP server even though the underlying API
            doesn't support MCP OAuth.
          </Type>
          <Type muted small className="mb-4 font-medium">
            Getting proxy settings correct can be tricky. Need help?
            <Link
              external
              to="https://calendly.com/d/ctgg-5dv-3kw/intro-to-gram-call"
            >
              Book a meeting
            </Link>
          </Type>

          {discoveredOAuth && !state.prefilled && (
            <div className="border-border bg-muted/50 mb-4 flex items-start justify-between gap-4 rounded-md border p-4">
              <div>
                <Type small className="font-medium">
                  OAuth detected from {discoveredOAuth.name}
                </Type>
                <Type muted small className="mt-1">
                  We discovered OAuth {discoveredOAuth.version} metadata from
                  this server. You can use it to pre-fill the endpoints below.
                </Type>
              </div>
              <Button
                size="sm"
                variant="secondary"
                onClick={() =>
                  dispatch({
                    type: "APPLY_DISCOVERED",
                    discoveredOAuth,
                  })
                }
              >
                Apply
              </Button>
            </div>
          )}
          {state.prefilled && (
            <div className="border-border bg-muted/50 mb-4 rounded-md border p-4">
              <Type small className="font-medium">
                Pre-filled from detected OAuth metadata
              </Type>
              <Type muted small className="mt-1">
                This form has been pre-filled with information Speakeasy
                detected about this server's OAuth requirements. Please review
                carefully and refer to the MCP server or API's documentation to
                confirm these values are correct.
              </Type>
            </div>
          )}

          {state.error && (
            <Type className="mb-4 text-sm text-red-500!">{state.error}</Type>
          )}

          <Stack gap={4}>
            <div>
              <Type className="mb-2 font-medium">OAuth Proxy Server Slug</Type>
              <Input
                placeholder="my-oauth-proxy"
                value={state.slug}
                onChange={(v: string) =>
                  dispatch({ type: "UPDATE_FIELD", field: "slug", value: v })
                }
                maxLength={40}
                disabled={editMode}
              />
            </div>

            <div>
              <Type className="mb-2 font-medium">Authorization Endpoint</Type>
              <Input
                placeholder="https://provider.com/oauth/authorize"
                value={state.authorizationEndpoint}
                onChange={(v: string) =>
                  dispatch({
                    type: "UPDATE_FIELD",
                    field: "authorizationEndpoint",
                    value: v,
                  })
                }
              />
            </div>

            <div>
              <Type className="mb-2 font-medium">Token Endpoint</Type>
              <Input
                placeholder="https://provider.com/oauth/token"
                value={state.tokenEndpoint}
                onChange={(v: string) =>
                  dispatch({
                    type: "UPDATE_FIELD",
                    field: "tokenEndpoint",
                    value: v,
                  })
                }
              />
            </div>

            <div>
              <Type className="mb-2 font-medium">Scopes (comma-separated)</Type>
              <Input
                placeholder="read, write, openid"
                value={state.scopes}
                onChange={(v: string) =>
                  dispatch({
                    type: "UPDATE_FIELD",
                    field: "scopes",
                    value: v,
                  })
                }
              />
            </div>

            <div>
              <Type className="mb-2 font-medium">Audience (optional)</Type>
              <Input
                placeholder="https://api.example.com"
                value={state.audience}
                onChange={(v: string) =>
                  dispatch({
                    type: "UPDATE_FIELD",
                    field: "audience",
                    value: v,
                  })
                }
              />
              <Type muted small className="mt-1">
                The audience parameter sent to the upstream OAuth provider.
                Required by some providers (e.g. Auth0) to return JWT access
                tokens.
              </Type>
            </div>

            <div>
              <Type className="mb-2 font-medium">
                Token Endpoint Auth Method
              </Type>
              <select
                className="bg-background w-full rounded border px-3 py-2"
                value={state.tokenAuthMethod}
                onChange={(e) =>
                  dispatch({
                    type: "UPDATE_FIELD",
                    field: "tokenAuthMethod",
                    value: e.target.value,
                  })
                }
              >
                <option value="client_secret_post">client_secret_post</option>
                <option value="client_secret_basic">client_secret_basic</option>
                <option value="none">none</option>
              </select>
            </div>
          </Stack>
        </div>
      </div>

      <Dialog.Footer className="flex justify-between">
        <Button
          variant="secondary"
          onClick={() => {
            if (editMode) {
              onClose();
            } else {
              dispatch({ type: "BACK" });
            }
          }}
        >
          {editMode ? "Cancel" : "Back"}
        </Button>
        <div className="ml-auto">
          <Button
            onClick={editMode ? onEditSubmit : onNext}
            disabled={
              (editMode && isEditPending) ||
              !state.slug.trim() ||
              !state.authorizationEndpoint.trim() ||
              !state.tokenEndpoint.trim()
            }
          >
            {editMode ? (isEditPending ? "Saving..." : "Save changes") : "Next"}
          </Button>
        </div>
      </Dialog.Footer>
    </>
  );
}

function ProxyCredentialsForm({
  state,
  dispatch,
  isSubmitting,
  onSubmit,
  attachedEnvironmentName,
  environmentsLink,
}: {
  state: Extract<WizardState, { step: "oauth_proxy_client_credentials_form" }>;
  dispatch: React.Dispatch<WizardAction>;
  isSubmitting: boolean;
  onSubmit: () => void;
  attachedEnvironmentName: string | null;
  environmentsLink: React.ReactNode;
}) {
  return (
    <>
      <div className="max-h-[60vh] space-y-4 overflow-auto">
        <div>
          <Type muted small className="mb-4">
            Enter the client credentials from your OAuth provider. These will be
            stored securely in a new environment created for this proxy.
          </Type>

          {attachedEnvironmentName && (
            <div className="border-border bg-muted/50 mb-4 flex items-start gap-3 rounded-md border p-4">
              <AlertTriangle className="text-muted-foreground mt-0.5 h-4 w-4 shrink-0" />
              <div>
                <Type small className="font-medium">
                  Existing environment will be detached
                </Type>
                <Type muted small className="mt-1">
                  The environment "{attachedEnvironmentName}" is currently
                  attached to this MCP server. It will be detached and replaced
                  with a new environment containing these OAuth credentials.
                </Type>
                <div className="mt-2">{environmentsLink}</div>
              </div>
            </div>
          )}

          {state.error && (
            <Type className="mb-4 text-sm text-red-500!">{state.error}</Type>
          )}

          <Stack gap={4}>
            <div>
              <Type className="mb-2 font-medium">Client ID</Type>
              <Input
                placeholder="your-client-id"
                value={state.clientId}
                onChange={(v: string) =>
                  dispatch({
                    type: "UPDATE_FIELD",
                    field: "clientId",
                    value: v,
                  })
                }
              />
            </div>

            <div>
              <Type className="mb-2 font-medium">Client Secret</Type>
              <Input
                placeholder="your-client-secret"
                value={state.clientSecret}
                onChange={(v: string) =>
                  dispatch({
                    type: "UPDATE_FIELD",
                    field: "clientSecret",
                    value: v,
                  })
                }
                type="password"
              />
            </div>
          </Stack>
        </div>
      </div>

      <Dialog.Footer className="flex justify-between">
        <Button variant="secondary" onClick={() => dispatch({ type: "BACK" })}>
          Back
        </Button>
        <div className="ml-auto">
          <Button
            onClick={onSubmit}
            disabled={
              isSubmitting ||
              !state.clientId.trim() ||
              !state.clientSecret.trim()
            }
          >
            {isSubmitting ? "Configuring..." : "Configure OAuth Proxy"}
          </Button>
        </div>
      </Dialog.Footer>
    </>
  );
}

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
  const setMcpMetadataMutation = useMcpMetadataSetMutation();

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
                // Chain: attach env first, then create proxy.
                // Running these in parallel caused the proxy onSuccess
                // (which closes the modal and invalidates queries) to
                // fire before the env attachment was persisted, leaving
                // the page showing "NO ENV ATTACHED".
                setMcpMetadataMutation.mutate(
                  {
                    request: {
                      setMcpMetadataRequestBody: {
                        ...mcpMetadata,
                        toolsetSlug,
                        defaultEnvironmentId: env.id,
                        environmentConfigs:
                          mcpMetadata?.environmentConfigs || [],
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
                  },
                );
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
    mcpMetadata,
    environments,
    createEnvironmentMutation,
    updateEnvironmentMutation,
    setMcpMetadataMutation,
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
    setMcpMetadataMutation.isPending ||
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
