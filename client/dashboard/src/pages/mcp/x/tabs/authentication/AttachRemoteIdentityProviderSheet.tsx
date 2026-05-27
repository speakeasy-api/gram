import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Sheet,
  SheetContent,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Type } from "@/components/ui/type";
import { useFetcher } from "@/contexts/Fetcher";
import { useSdkClient } from "@/contexts/Sdk";
import {
  buildUserSessionResourceSlug,
  DEFAULT_USER_SESSION_DURATION_HOURS,
} from "@/lib/externalMcpUserSessions";
import { proxyRegisterUpstreamClient } from "@/lib/proxyRegisterUpstreamClient";
import type {
  McpServer,
  RemoteSessionIssuer,
  UserSessionIssuer,
} from "@gram/client/models/components";
import { CreateRemoteSessionClientFormTokenEndpointAuthMethod } from "@gram/client/models/components";
import {
  invalidateAllGetMcpServer,
  invalidateAllMcpServers,
  invalidateAllRemoteSessionClients,
  invalidateAllRemoteSessionIssuers,
  invalidateAllUserSessionIssuers,
} from "@gram/client/react-query/index.js";
import { Alert, Button, Stack } from "@speakeasy-api/moonshine";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import {
  ClientCredentialsFields,
  DcrNotice,
  EndpointsFields,
  IssuerUrlField,
  OverridesFields,
  TokenEndpointAuthMethodField,
} from "./IssuerFormFields";
import { narrowTokenEndpointAuthMethod, parseScopes } from "./issuerFormUtils";
import { useIssuerDiscovery } from "./useIssuerDiscovery";

type Mode = "select" | "new";

export function AttachRemoteIdentityProviderSheet({
  open,
  onOpenChange,
  mcpServer,
  userSessionIssuer,
  selectableIssuers,
  initialIssuerUrl,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  mcpServer: McpServer;
  // null when the MCP server has no user_session_issuer linked yet — the
  // first add also creates one and links it via updateMcpServer.
  userSessionIssuer: UserSessionIssuer | null;
  // Project-scope remote_session_issuers that are not already associated with
  // userSessionIssuer. Empty list hides the "Select existing" mode.
  selectableIssuers: RemoteSessionIssuer[];
  // When the caller opens via "Start With Discovered Configuration", this is the
  // authorization_servers[0] entry from the RFC 9728 probe. The sheet starts
  // in "new" mode and runs RFC 8414 discovery against this URL to prefill the
  // upstream endpoints.
  initialIssuerUrl?: string;
}) {
  const client = useSdkClient();
  const { fetch: authedFetch } = useFetcher();
  const queryClient = useQueryClient();

  const hasSelectable = selectableIssuers.length > 0;
  const [mode, setMode] = useState<Mode>(
    initialIssuerUrl || !hasSelectable ? "new" : "select",
  );
  const [selectedIssuerId, setSelectedIssuerId] = useState<string>("");

  const [slug, setSlug] = useState("");
  // When the operator hasn't manually edited Slug, we keep it in lockstep
  // with a slugified form of the Issuer URL hostname (similar to the remote
  // MCP source slug auto-derivation). Once they type into Slug we treat it
  // as their value and stop overwriting it.
  const [slugDirty, setSlugDirty] = useState(false);

  // Issuer URL + 4 endpoint fields + discovery state live in the shared
  // hook. The hook seeds from `null` here — the Attach sheet starts blank
  // and the "Start With Discovered Configuration" path triggers an explicit
  // runDiscover below.
  const discovery = useIssuerDiscovery(null);
  const {
    issuerUrl,
    setIssuerUrl,
    authorizationEndpoint,
    setAuthorizationEndpoint,
    tokenEndpoint,
    setTokenEndpoint,
    registrationEndpoint,
    setRegistrationEndpoint,
    jwksUri,
    setJwksUri,
    discoveredSnapshot,
    discoverPending,
    discoverError,
    clearDiscoverError,
    runDiscover,
    handleResetEndpoints,
    resetEndpointState,
    showDiscoverControls,
    showResetControls,
    endpointWarnings,
  } = discovery;

  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [tokenEndpointAuthMethod, setTokenEndpointAuthMethod] = useState<
    CreateRemoteSessionClientFormTokenEndpointAuthMethod | ""
  >("");

  // Per-client OAuth dance overrides. Both apply regardless of how the client
  // was registered (DCR vs manual). scopeOverride is comma-separated, parsed
  // into string[] on submit. audienceOverride is RFC 8707 — only ever sent
  // at flow time, never at DCR registration.
  const [scopeOverride, setScopeOverride] = useState("");
  const [audienceOverride, setAudienceOverride] = useState("");

  const attachMutation = useMutation({
    mutationFn: async (): Promise<{
      unsupportedDcrAuthMethod: string | null;
    }> => {
      // Step 1: ensure a user_session_issuer exists. First-add auto-creates
      // one with the conservative interactive challenge mode and a 2-week
      // session lifetime — these match the wire-user-session-issuer defaults.
      let issuerId = userSessionIssuer?.id;
      if (!issuerId) {
        const created = await client.userSessionIssuers.create({
          createUserSessionIssuerForm: {
            slug: buildUserSessionResourceSlug(mcpServer.slug ?? "mcp"),
            authnChallengeMode: "interactive",
            sessionDurationHours: DEFAULT_USER_SESSION_DURATION_HOURS,
          },
        });
        issuerId = created.id;
      }

      // Step 2: resolve the remote_session_issuer — pick existing or create a
      // new one from the form fields. Empty endpoint inputs are omitted so
      // the backend can store them as null rather than empty strings.
      let resolvedIssuer: RemoteSessionIssuer | undefined;
      let remoteIssuerId: string;
      if (mode === "select") {
        remoteIssuerId = selectedIssuerId;
        resolvedIssuer = selectableIssuers.find(
          (issuer) => issuer.id === selectedIssuerId,
        );
      } else {
        const created = await client.remoteSessionIssuers.create({
          createRemoteSessionIssuerForm: {
            slug: slug.trim(),
            issuer: issuerUrl.trim(),
            authorizationEndpoint: authorizationEndpoint.trim() || undefined,
            tokenEndpoint: tokenEndpoint.trim() || undefined,
            registrationEndpoint: registrationEndpoint.trim() || undefined,
            jwksUri: jwksUri.trim() || undefined,
            // RFC 8414 metadata arrays are NOT NULL on the server side. When
            // discovery ran we forward what it returned; when the operator
            // typed everything by hand we send empty arrays so the upstream
            // matches "issuer did not advertise these" semantics.
            scopesSupported: discoveredSnapshot?.scopesSupported ?? [],
            grantTypesSupported: discoveredSnapshot?.grantTypesSupported ?? [],
            responseTypesSupported:
              discoveredSnapshot?.responseTypesSupported ?? [],
            tokenEndpointAuthMethodsSupported:
              discoveredSnapshot?.tokenEndpointAuthMethodsSupported ?? [],
          },
        });
        remoteIssuerId = created.id;
        resolvedIssuer = created;
      }

      // Step 3: obtain client credentials. When DCR is available the proxy
      // registers Gram with the upstream and hands back client_id /
      // client_secret; otherwise we use what the operator typed. The scope
      // override (if set) is forwarded to the registration call so the
      // upstream registers the client with that scope set. DCR fires in both
      // modes — in Add-new it uses the form field, in Select-existing it
      // uses the picked issuer's saved registration_endpoint.
      const parsedScopes = parseScopes(scopeOverride);
      const trimmedAudience = audienceOverride.trim();
      const registrationEndpointForDcr =
        resolvedIssuer?.registrationEndpoint ?? "";
      let clientCredentials: {
        clientId: string;
        clientSecret?: string;
        tokenEndpointAuthMethod?: CreateRemoteSessionClientFormTokenEndpointAuthMethod;
      };
      // Tracks when DCR returns an auth method the SDK enum doesn't model
      // (e.g. `none`, `private_key_jwt`). We swallow it for the local client
      // record but warn the operator after success so they understand why
      // their client may not behave as the upstream advertised.
      let unsupportedDcrAuthMethod: string | null = null;
      if (dcrAvailable && registrationEndpointForDcr) {
        const registered = await proxyRegisterUpstreamClient(authedFetch, {
          registrationEndpoint: registrationEndpointForDcr,
          // RFC 7591 §2: scope is a space-separated string at registration
          // time. Only forward if the operator typed an override.
          scope: parsedScopes.length > 0 ? parsedScopes.join(" ") : undefined,
          // Forward the operator's auth-method preference so DCR registers
          // the client with that method. Omit when blank so the upstream
          // picks its own default.
          tokenEndpointAuthMethod: tokenEndpointAuthMethod || undefined,
        });
        const narrowedDcrMethod = narrowTokenEndpointAuthMethod(
          registered.tokenEndpointAuthMethod,
        );
        if (registered.tokenEndpointAuthMethod && !narrowedDcrMethod) {
          unsupportedDcrAuthMethod = registered.tokenEndpointAuthMethod;
        }
        clientCredentials = {
          clientId: registered.clientId,
          clientSecret: registered.clientSecret || undefined,
          // Prefer the upstream-confirmed method when it's one we support;
          // fall back to the operator's selection so a known-good value
          // still lands on the client record even if DCR echoed back
          // something unknown. Treat the operator's "" sentinel (no
          // selection) as undefined so the API sees an omitted value.
          tokenEndpointAuthMethod:
            narrowedDcrMethod ?? (tokenEndpointAuthMethod || undefined),
        };
      } else {
        clientCredentials = {
          clientId: clientId.trim(),
          clientSecret: clientSecret.trim() || undefined,
          tokenEndpointAuthMethod: tokenEndpointAuthMethod || undefined,
        };
      }

      // Step 4: create the remote_session_client binding the resolved issuer
      // to the user_session_issuer. Scope/audience overrides are stored on
      // the client and consumed at OAuth dance time.
      await client.remoteSessionClients.create({
        createRemoteSessionClientForm: {
          remoteSessionIssuerId: remoteIssuerId,
          userSessionIssuerId: issuerId,
          clientId: clientCredentials.clientId,
          clientSecret: clientCredentials.clientSecret,
          tokenEndpointAuthMethod: clientCredentials.tokenEndpointAuthMethod,
          scope: parsedScopes.length > 0 ? parsedScopes : undefined,
          audience: trimmedAudience || undefined,
        },
      });

      // Step 5: on first-add, point the MCP server at the freshly-created
      // user_session_issuer and set visibility to private so the server
      // begins serving traffic. updateMcpServer is a full-record replace,
      // so re-send the existing UUID references alongside the update.
      if (!userSessionIssuer) {
        await client.mcpServers.update({
          updateMcpServerForm: {
            id: mcpServer.id,
            name: mcpServer.name ?? undefined,
            remoteMcpServerId: mcpServer.remoteMcpServerId ?? undefined,
            toolsetId: mcpServer.toolsetId ?? undefined,
            environmentId: mcpServer.environmentId ?? undefined,
            visibility: "private",
            userSessionIssuerId: issuerId,
          },
        });
      }

      return { unsupportedDcrAuthMethod };
    },
    onSuccess: async ({ unsupportedDcrAuthMethod }) => {
      await Promise.all([
        invalidateAllUserSessionIssuers(queryClient, { refetchType: "all" }),
        invalidateAllRemoteSessionIssuers(queryClient, { refetchType: "all" }),
        invalidateAllRemoteSessionClients(queryClient, { refetchType: "all" }),
        invalidateAllGetMcpServer(queryClient, { refetchType: "all" }),
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
      ]);

      toast.success("Identity provider attached");
      if (unsupportedDcrAuthMethod) {
        toast.warning(
          `Upstream issuer reported token endpoint auth method "${unsupportedDcrAuthMethod}", which the platform doesn't model. The client falls back to ${tokenEndpointAuthMethod || "client_secret_basic"} — adjust on the identity provider's Modify sheet if needed.`,
        );
      }
      onOpenChange(false);
    },
    onError: (error) => {
      // Backend Create messages aren't always actionable (e.g. the generic
      // "create remote session issuer" fallback) but specific ones like
      // "an identity provider with slug already exists" or 4xx validation
      // errors absolutely are — useMutation surfaces error.message via
      // attachMutation.error so we only need to log here.
      console.error("Attach identity provider failed", error);
    },
  });

  const submitting = attachMutation.isPending;
  const submitError = attachMutation.error
    ? attachMutation.error instanceof Error && attachMutation.error.message
      ? attachMutation.error.message
      : "An unexpected error occurred. Please try again."
    : null;
  const { reset: resetAttachMutation } = attachMutation;

  // Reset transient state whenever the sheet is reopened. The mcpServer slug
  // seeds the default new-issuer slug so most operators can submit without
  // touching the field, but we still allow editing.
  useEffect(() => {
    if (!open) return;
    setMode(initialIssuerUrl || !hasSelectable ? "new" : "select");
    setSelectedIssuerId("");
    // Seed the slug from the Issuer URL when we have one (the "Start With
    // Discovered Configuration" path). Otherwise fall back to the
    // mcpServer-based default. Either way slugDirty resets to false so the
    // operator's first keystroke in the field starts locking it in.
    setSlug(
      deriveSlugFromUrl(initialIssuerUrl ?? "") ??
        buildUserSessionResourceSlug(mcpServer.slug ?? "mcp"),
    );
    setSlugDirty(false);
    setIssuerUrl(initialIssuerUrl ?? "");
    resetEndpointState();
    clearDiscoverError();
    setClientId("");
    setClientSecret("");
    setTokenEndpointAuthMethod("");
    setScopeOverride("");
    setAudienceOverride("");
    resetAttachMutation();
  }, [
    open,
    mcpServer.slug,
    initialIssuerUrl,
    hasSelectable,
    setIssuerUrl,
    resetEndpointState,
    clearDiscoverError,
    resetAttachMutation,
  ]);

  // Auto-run discovery when the sheet opens with a seeded URL (came in via
  // "Start With Discovered Configuration"). Manual configure leaves this to the
  // explicit "Discover" button in the Endpoints section.
  useEffect(() => {
    if (!open || !initialIssuerUrl) return;
    void runDiscover(initialIssuerUrl);
  }, [open, initialIssuerUrl, runDiscover]);

  // Resolve the issuer record the operator picked in Select-existing mode.
  // We need it to know whether the picked issuer supports DCR and to pull
  // its registration_endpoint at submit time.
  const selectedIssuer =
    mode === "select"
      ? selectableIssuers.find((issuer) => issuer.id === selectedIssuerId)
      : undefined;

  // DCR is offered automatically when a registration_endpoint is present.
  // In Add-new the value comes from the form (filled by discovery or typed);
  // in Select-existing it comes from the picked issuer record. Either way we
  // hide the manual client_id / client_secret form and call proxy-register
  // on submit.
  const dcrAvailable =
    mode === "new"
      ? registrationEndpoint.trim().length > 0
      : !!selectedIssuer?.registrationEndpoint;

  const submittable = useMemo(() => {
    if (mode === "select") return !!selectedIssuerId;
    if (!slug.trim() || !issuerUrl.trim()) return false;
    if (!dcrAvailable && !clientId.trim()) return false;
    return true;
  }, [mode, selectedIssuerId, slug, issuerUrl, dcrAvailable, clientId]);

  const handleSubmit = () => {
    if (!submittable || submitting) return;
    attachMutation.mutate();
  };

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-[560px] flex-col sm:max-w-[560px]"
      >
        <SheetHeader className="px-6 pt-6 pb-0">
          <SheetTitle className="text-lg font-semibold">
            Attach Remote Identity Provider
          </SheetTitle>
        </SheetHeader>

        <div className="flex-1 space-y-6 overflow-y-auto px-6 py-6">
          {hasSelectable && <ModeSwitch mode={mode} onChange={setMode} />}

          {mode === "select" ? (
            <SelectExistingFields
              selectableIssuers={selectableIssuers}
              selectedIssuerId={selectedIssuerId}
              onChange={setSelectedIssuerId}
            />
          ) : (
            <Stack gap={4}>
              <IssuerUrlField
                issuerUrl={issuerUrl}
                onIssuerUrlChange={(value) => {
                  setIssuerUrl(value);
                  // A stale error from a previous URL would be misleading once
                  // the operator starts typing a new target; clear it so the
                  // next Discover click starts fresh.
                  clearDiscoverError();
                  // Auto-derive the slug from the hostname while the operator
                  // hasn't customized it. We swallow URL parse failures so the
                  // slug stays stable while a partial URL is being typed.
                  if (!slugDirty) {
                    const derived = deriveSlugFromUrl(value);
                    if (derived) setSlug(derived);
                  }
                  // When the URL diverges from a settled discovery, every
                  // downstream field (endpoints, credentials, scope/audience,
                  // DCR-vs-manual decision) was tied to that prior URL and is
                  // now stale. Reset the form so the operator runs Discover
                  // again against the new target and gets a coherent state.
                  if (
                    discoveredSnapshot &&
                    value.trim() !== discoveredSnapshot.url
                  ) {
                    resetEndpointState();
                    setClientId("");
                    setClientSecret("");
                    setTokenEndpointAuthMethod("");
                    setScopeOverride("");
                    setAudienceOverride("");
                  }
                }}
              />

              <Stack gap={2}>
                <Label className="text-muted-foreground text-xs">Slug</Label>
                <Input
                  value={slug}
                  onChange={(value) => {
                    setSlug(value);
                    setSlugDirty(true);
                  }}
                  placeholder="my-identity-provider"
                />
                <Type muted small>
                  Project-unique identifier for this identity provider.
                  Auto-derived from the Issuer URL until you edit it.
                </Type>
              </Stack>

              <EndpointsFields
                issuerUrl={issuerUrl}
                authorizationEndpoint={authorizationEndpoint}
                tokenEndpoint={tokenEndpoint}
                registrationEndpoint={registrationEndpoint}
                jwksUri={jwksUri}
                endpointWarnings={endpointWarnings}
                discoverPending={discoverPending}
                discoverError={discoverError}
                showDiscoverControls={showDiscoverControls}
                showResetControls={showResetControls}
                onAuthorizationEndpointChange={setAuthorizationEndpoint}
                onTokenEndpointChange={setTokenEndpoint}
                onRegistrationEndpointChange={setRegistrationEndpoint}
                onJwksUriChange={setJwksUri}
                onDiscover={() => runDiscover(issuerUrl)}
                onResetEndpoints={handleResetEndpoints}
              />
            </Stack>
          )}

          {dcrAvailable ? (
            <>
              <DcrNotice />
              {/* token_endpoint_auth_method stays editable even in DCR so
                  operators can override the upstream-assigned default. The
                  selected value is forwarded into the proxy-register call so
                  the upstream registers the client with the desired method. */}
              <TokenEndpointAuthMethodField
                value={tokenEndpointAuthMethod}
                onChange={setTokenEndpointAuthMethod}
              />
            </>
          ) : (
            <ClientCredentialsFields
              clientId={clientId}
              clientSecret={clientSecret}
              tokenEndpointAuthMethod={tokenEndpointAuthMethod}
              onClientIdChange={setClientId}
              onClientSecretChange={setClientSecret}
              onTokenEndpointAuthMethodChange={setTokenEndpointAuthMethod}
            />
          )}

          <OverridesFields
            scopeOverride={scopeOverride}
            audienceOverride={audienceOverride}
            onScopeOverrideChange={setScopeOverride}
            onAudienceOverrideChange={setAudienceOverride}
          />

          {submitError && (
            <Alert variant="error" dismissible={false}>
              {submitError}
            </Alert>
          )}
        </div>

        <SheetFooter className="flex-row items-center justify-end gap-2 border-t px-6 py-4">
          <Button
            variant="secondary"
            disabled={submitting}
            onClick={() => onOpenChange(false)}
          >
            <Button.Text>Cancel</Button.Text>
          </Button>
          <Button
            variant="primary"
            disabled={!submittable || submitting}
            onClick={handleSubmit}
          >
            <Button.Text>
              {submitting ? "Attaching…" : "Attach Identity Provider"}
            </Button.Text>
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}

function ModeSwitch({
  mode,
  onChange,
}: {
  mode: Mode;
  onChange: (next: Mode) => void;
}) {
  return (
    <Stack direction="horizontal" gap={2}>
      <Button
        variant={mode === "select" ? "primary" : "secondary"}
        onClick={() => onChange("select")}
      >
        <Button.Text>Select existing</Button.Text>
      </Button>
      <Button
        variant={mode === "new" ? "primary" : "secondary"}
        onClick={() => onChange("new")}
      >
        <Button.Text>Add new</Button.Text>
      </Button>
    </Stack>
  );
}

function SelectExistingFields({
  selectableIssuers,
  selectedIssuerId,
  onChange,
}: {
  selectableIssuers: RemoteSessionIssuer[];
  selectedIssuerId: string;
  onChange: (id: string) => void;
}) {
  return (
    <Stack gap={2}>
      <Label className="text-muted-foreground text-xs">Identity Provider</Label>
      <Select value={selectedIssuerId} onValueChange={onChange}>
        <SelectTrigger>
          <SelectValue placeholder="Choose an identity provider…" />
        </SelectTrigger>
        <SelectContent>
          {selectableIssuers.map((issuer) => (
            <SelectItem key={issuer.id} value={issuer.id}>
              {issuer.slug} — {issuer.issuer}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Type muted small>
        Pick an identity provider already configured on this project.
      </Type>
    </Stack>
  );
}

// Derive a project-unique slug from the Issuer URL's hostname. Mirrors the
// hyphen-style transform used by buildUserSessionResourceSlug's internal
// slugify helper so the auto-filled value matches what an operator would
// reasonably hand-write. Returns null for unparseable URLs — the caller
// keeps the prior slug in that case so partial typing doesn't blow it away.
function deriveSlugFromUrl(url: string): string | null {
  const trimmed = url.trim();
  if (!trimmed) return null;
  try {
    const host = new URL(trimmed).hostname;
    if (!host) return null;
    const slug = host
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "");
    return slug || null;
  } catch {
    return null;
  }
}
