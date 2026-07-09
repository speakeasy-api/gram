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
import { deriveRemoteSessionIssuerNameFromUrl } from "@/lib/sources";
import { remoteSessionClientDisplayName } from "@/pages/remote-identity-providers/clientDisplay";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { CreateRemoteSessionClientFormTokenEndpointAuthMethod } from "@gram/client/models/components/createremotesessionclientform.js";
import { invalidateAllRemoteSessionClients } from "@gram/client/react-query/remoteSessionClients.js";
import { invalidateAllRemoteSessionIssuers } from "@gram/client/react-query/remoteSessionIssuers.js";
import { invalidateAllUserSessionIssuers } from "@gram/client/react-query/userSessionIssuers.js";
import { Alert, Button, Input, Stack } from "@/components/ui/moonshine";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import type { AuthTarget } from "./authTarget";
import {
  ClientTypeFields,
  EndpointsFields,
  IssuerUrlField,
  OverridesFields,
} from "./IssuerFormFields";
import {
  availableClientTypes,
  type ClientType,
  deriveSlugFromUrl,
  narrowTokenEndpointAuthMethod,
  parseScopes,
  pickPreferredAuthMethod,
} from "./issuerFormUtils";
import { useAllRemoteSessionClients } from "./useAllRemoteSessionClients";
import { useIssuerDiscovery } from "./useIssuerDiscovery";

type Mode = "select" | "new";

export function AttachRemoteIdentityProviderSheet({
  open,
  onOpenChange,
  target,
  userSessionIssuer,
  selectableIssuers,
  initialIssuerUrl,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  // The MCP server or toolset the issuer gets linked to.
  target: AuthTarget;
  // null when the target has no issuer yet — the first add creates one and
  // links it via target.linkUserSessionIssuer.
  userSessionIssuer: UserSessionIssuer | null;
  // remote_session_issuers (organization-level and same-project) that are not
  // already associated with userSessionIssuer. Empty list hides the issuer
  // "Select existing" mode.
  selectableIssuers: RemoteSessionIssuer[];
  // When the caller opens via "Start With Discovered Configuration", this is the
  // authorization_servers[0] entry from the RFC 9728 probe. The sheet starts
  // in "new" mode and runs RFC 8414 discovery against this URL to prefill the
  // upstream endpoints.
  initialIssuerUrl?: string;
}): JSX.Element {
  const client = useSdkClient();
  const { fetch: authedFetch } = useFetcher();
  const queryClient = useQueryClient();

  const hasSelectable = selectableIssuers.length > 0;
  const [mode, setMode] = useState<Mode>(
    initialIssuerUrl || !hasSelectable ? "new" : "select",
  );
  const [selectedIssuerId, setSelectedIssuerId] = useState<string>("");

  // Optional display name. Auto-derived from the Issuer URL hostname (like Slug
  // below) until the operator edits it, after which nameDirty locks it to their
  // value. Empty submits as undefined so the backend stores NULL and consumers
  // fall back to the issuer URL.
  const [name, setName] = useState("");
  const [nameDirty, setNameDirty] = useState(false);

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

  // Session Client section state. The client toggle mirrors the issuer toggle:
  // when the resolved issuer already has attachable clients the operator can
  // bind an existing one (attachUserSessionIssuer) instead of registering a
  // new one. clientType drives how a new client is created (DCR / CIMD /
  // Manual); it is reset to the recommended default in an effect below.
  const [clientMode, setClientMode] = useState<Mode>("select");
  const [selectedClientId, setSelectedClientId] = useState("");
  const [clientType, setClientType] = useState<ClientType>("manual");

  // Resolve the issuer record the operator picked in Select-existing mode. We
  // need it to know whether the picked issuer supports DCR/CIMD and to pull its
  // registration_endpoint at submit time.
  const selectedIssuer =
    mode === "select"
      ? selectableIssuers.find((issuer) => issuer.id === selectedIssuerId)
      : undefined;

  // DCR and CIMD availability drive the Client Type selector. In Add-new the
  // values come from the form (filled by discovery or typed); in
  // Select-existing they come from the picked issuer record.
  const dcrAvailable =
    mode === "new"
      ? registrationEndpoint.trim().length > 0
      : !!selectedIssuer?.registrationEndpoint;
  const cimdAvailable =
    mode === "new"
      ? (discoveredSnapshot?.clientIdMetadataDocumentSupported ?? false)
      : !!selectedIssuer?.clientIdMetadataDocumentSupported;
  const clientTypes = useMemo(
    () => availableClientTypes({ dcrAvailable, cimdAvailable }),
    [dcrAvailable, cimdAvailable],
  );

  // Existing clients of the picked issuer (this project's clients, whether the
  // issuer is organization-level or project-level). Only an existing issuer can
  // have clients, so the walk is disabled in Add-new-issuer mode. Filter out
  // any already bound to this user_session_issuer — none today, since
  // selectableIssuers excludes already-associated issuers, but defensive
  // against that invariant changing.
  const { items: issuerClients, isLoading: isLoadingIssuerClients } =
    useAllRemoteSessionClients(
      { remoteSessionIssuerId: selectedIssuerId },
      { enabled: mode === "select" && !!selectedIssuerId },
    );
  const attachableClients = useMemo(
    () =>
      issuerClients.filter(
        (candidate) =>
          !userSessionIssuer ||
          !candidate.userSessionIssuerIds.includes(userSessionIssuer.id),
      ),
    [issuerClients, userSessionIssuer],
  );

  // The Session Client toggle only appears for an existing issuer that has
  // attachable clients; otherwise the sheet shows the Add-new client form
  // directly (a brand-new issuer never has clients).
  const clientToggleVisible = attachableClients.length > 0;
  const effectiveClientMode: Mode = clientToggleVisible ? clientMode : "new";
  const selectedClient = attachableClients.find(
    (candidate) => candidate.id === selectedClientId,
  );

  // The Session Client section stays hidden until an identity provider is
  // determined — an existing one is picked, or a new one's Issuer URL has been
  // entered — since every client choice depends on that provider.
  const issuerResolved =
    mode === "select" ? !!selectedIssuerId : issuerUrl.trim().length > 0;

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
            slug: buildUserSessionResourceSlug(target.slug),
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
            name: name.trim() || undefined,
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
            // CIMD support parsed during discovery; persisted so the issuer can
            // offer the CIMD client type. False when discovery did not run.
            clientIdMetadataDocumentSupported:
              discoveredSnapshot?.clientIdMetadataDocumentSupported ?? false,
          },
        });
        remoteIssuerId = created.id;
        resolvedIssuer = created;
      }

      // Step 3: bind a remote_session_client to the user_session_issuer.
      // Either attach an existing client (a join-table insert) or create a new
      // one in the chosen mode (DCR / CIMD / Manual). Scope/audience overrides
      // apply only to newly created clients; attaching reuses the selected
      // client's stored configuration.
      const parsedScopes = parseScopes(scopeOverride);
      const trimmedAudience = audienceOverride.trim();
      // Tracks when DCR returns an auth method the SDK enum doesn't model
      // (e.g. `private_key_jwt`). We swallow it for the local client record
      // but warn the operator after success so they understand why their
      // client may not behave as the upstream advertised.
      let unsupportedDcrAuthMethod: string | null = null;

      if (effectiveClientMode === "select") {
        // Attach the picked existing client to this user_session_issuer.
        await client.remoteSessionClients.attachUserSessionIssuer({
          attachUserSessionIssuerForm: {
            id: selectedClientId,
            userSessionIssuerId: issuerId,
          },
        });
      } else if (clientType === "cimd") {
        // CIMD: the platform generates the client_id and hosts the metadata
        // document, so there are no credentials to collect.
        await client.remoteSessionClients.createCimd({
          createCimdForm: {
            remoteSessionIssuerId: remoteIssuerId,
            userSessionIssuerIds: [issuerId],
            scope: parsedScopes.length > 0 ? parsedScopes : undefined,
            audience: trimmedAudience || undefined,
          },
        });
      } else {
        // DCR proxies a registration to the upstream issuer for a fresh
        // client_id / client_secret; Manual uses what the operator typed.
        let clientCredentials: {
          clientId: string;
          clientSecret?: string;
          tokenEndpointAuthMethod?: CreateRemoteSessionClientFormTokenEndpointAuthMethod;
        };
        if (clientType === "dcr") {
          const registered = await proxyRegisterUpstreamClient(authedFetch, {
            registrationEndpoint: resolvedIssuer?.registrationEndpoint ?? "",
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

        // Create the remote_session_client binding the resolved issuer to the
        // user_session_issuer. Scope/audience overrides are stored on the
        // client and consumed at OAuth dance time.
        await client.remoteSessionClients.create({
          createRemoteSessionClientForm: {
            remoteSessionIssuerId: remoteIssuerId,
            userSessionIssuerIds: [issuerId],
            clientId: clientCredentials.clientId,
            clientSecret: clientCredentials.clientSecret,
            tokenEndpointAuthMethod: clientCredentials.tokenEndpointAuthMethod,
            scope: parsedScopes.length > 0 ? parsedScopes : undefined,
            audience: trimmedAudience || undefined,
          },
        });
      }

      // Step 4: on first-add, link the target to the new issuer. How the link
      // is stored (and side effects like flipping a server private) is the
      // target's business.
      if (!userSessionIssuer) {
        await target.linkUserSessionIssuer(issuerId);
      }

      return { unsupportedDcrAuthMethod };
    },
    onSuccess: async ({ unsupportedDcrAuthMethod }) => {
      await Promise.all([
        invalidateAllUserSessionIssuers(queryClient, { refetchType: "all" }),
        invalidateAllRemoteSessionIssuers(queryClient, { refetchType: "all" }),
        invalidateAllRemoteSessionClients(queryClient, { refetchType: "all" }),
        target.invalidate(queryClient),
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

  // Reset transient state whenever the sheet is reopened. The target slug
  // seeds the default new-issuer slug so most operators can submit without
  // touching the field, but we still allow editing.
  useEffect(() => {
    if (!open) return;
    setMode(initialIssuerUrl || !hasSelectable ? "new" : "select");
    setSelectedIssuerId("");
    // Seed the slug from the Issuer URL when we have one (the "Start With
    // Discovered Configuration" path). Otherwise fall back to the
    // target-based default. Either way slugDirty resets to false so the
    // operator's first keystroke in the field starts locking it in.
    setSlug(
      deriveSlugFromUrl(initialIssuerUrl ?? "") ??
        buildUserSessionResourceSlug(target.slug),
    );
    setSlugDirty(false);
    setName(deriveRemoteSessionIssuerNameFromUrl(initialIssuerUrl ?? "") ?? "");
    setNameDirty(false);
    setIssuerUrl(initialIssuerUrl ?? "");
    resetEndpointState();
    clearDiscoverError();
    setClientId("");
    setClientSecret("");
    setTokenEndpointAuthMethod("");
    setScopeOverride("");
    setAudienceOverride("");
    setClientMode("select");
    setSelectedClientId("");
    resetAttachMutation();
  }, [
    open,
    target.slug,
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
    runDiscover(initialIssuerUrl);
  }, [open, initialIssuerUrl, runDiscover]);

  // When discovery completes, auto-select the best supported auth method.
  // Preference: client_secret_basic > client_secret_post > none.
  // Guard against a late discovery response landing after the operator has
  // already changed the Issuer URL: a first discovery still in flight leaves
  // discoveredSnapshot null, so the URL-change reset can't catch it. Only
  // apply when the snapshot is for the URL currently in the form.
  useEffect(() => {
    if (!discoveredSnapshot) return;
    if (discoveredSnapshot.url !== issuerUrl.trim()) return;
    const preferred = pickPreferredAuthMethod(
      discoveredSnapshot.tokenEndpointAuthMethodsSupported,
    );
    if (!tokenEndpointAuthMethod && preferred) {
      setTokenEndpointAuthMethod(preferred);
    }
  }, [discoveredSnapshot, issuerUrl, tokenEndpointAuthMethod]);

  // Reset the new-client type to the recommended default whenever the issuer's
  // DCR/CIMD availability changes (a different issuer pick or a fresh
  // discovery), so the auto path stays pre-selected against the current issuer.
  useEffect(() => {
    setClientType(
      availableClientTypes({ dcrAvailable, cimdAvailable })[0] ?? "manual",
    );
  }, [dcrAvailable, cimdAvailable]);

  // A different issuer (or switching to Add-new issuer) means a different
  // client list; drop any stale existing-client selection.
  useEffect(() => {
    setSelectedClientId("");
  }, [selectedIssuerId, mode]);

  const submittable = useMemo(() => {
    // Issuer must be resolvable: an existing pick, or a complete new-issuer
    // form.
    if (mode === "select") {
      if (!selectedIssuerId) return false;
    } else if (!slug.trim() || !issuerUrl.trim()) {
      return false;
    }
    // Session client: attach an existing one, or complete the new-client form.
    if (effectiveClientMode === "select") return !!selectedClientId;
    // Manual requires a client_id; DCR mints one; CIMD needs none.
    if (clientType === "manual" && !clientId.trim()) return false;
    return true;
  }, [
    mode,
    selectedIssuerId,
    slug,
    issuerUrl,
    effectiveClientMode,
    selectedClientId,
    clientType,
    clientId,
  ]);

  const handleSubmit = () => {
    if (!submittable || submitting) return;
    attachMutation.mutate();
  };

  // The Session Client section body: its loading, select-existing, and add-new
  // branches, kept out of the JSX so the render stays flat.
  const clientToggle = clientToggleVisible ? (
    <ModeSwitch mode={clientMode} onChange={setClientMode} />
  ) : null;
  let clientSectionBody: JSX.Element;
  if (mode === "select" && selectedIssuerId && isLoadingIssuerClients) {
    clientSectionBody = (
      <Type muted small>
        Loading clients…
      </Type>
    );
  } else if (effectiveClientMode === "select") {
    clientSectionBody = (
      <Stack gap={4}>
        {clientToggle}
        <SelectExistingClientFields
          clients={attachableClients}
          selectedClientId={selectedClientId}
          onChange={setSelectedClientId}
          selectedClient={selectedClient}
        />
      </Stack>
    );
  } else {
    clientSectionBody = (
      <Stack gap={4}>
        {clientToggle}
        <ClientTypeFields
          availableTypes={clientTypes}
          clientType={clientType}
          onClientTypeChange={setClientType}
          clientId={clientId}
          clientSecret={clientSecret}
          tokenEndpointAuthMethod={tokenEndpointAuthMethod}
          onClientIdChange={setClientId}
          onClientSecretChange={setClientSecret}
          onTokenEndpointAuthMethodChange={setTokenEndpointAuthMethod}
        />
        <OverridesFields
          scopeOverride={scopeOverride}
          audienceOverride={audienceOverride}
          onScopeOverrideChange={setScopeOverride}
          onAudienceOverrideChange={setAudienceOverride}
        />
      </Stack>
    );
  }

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
          <Stack gap={4}>
            <SectionHeading
              title="Identity Provider"
              description="The upstream OAuth authorization server Gram delegates to."
            />
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
                    // Same auto-derive-until-edited behavior for the Display
                    // name, seeded from the URL hostname.
                    if (!nameDirty) {
                      const derivedName =
                        deriveRemoteSessionIssuerNameFromUrl(value);
                      if (derivedName) setName(derivedName);
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
                    onChange={(e) => {
                      setSlug(e.target.value);
                      setSlugDirty(true);
                    }}
                    placeholder="my-identity-provider"
                  />
                  <Type muted small>
                    Project-unique identifier for this identity provider.
                    Auto-derived from the Issuer URL until you edit it.
                  </Type>
                </Stack>

                <Stack gap={2}>
                  <Label className="text-muted-foreground text-xs">
                    Display name (optional)
                  </Label>
                  <Input
                    value={name}
                    onChange={(e) => {
                      setName(e.target.value);
                      setNameDirty(true);
                    }}
                    placeholder="My Identity Provider"
                  />
                  <Type muted small>
                    Friendly label shown in the dashboard. Auto-derived from the
                    Issuer URL until you edit it; falls back to the Issuer URL
                    when left blank.
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
                  onDiscover={() => {
                    runDiscover(issuerUrl);
                  }}
                  onResetEndpoints={handleResetEndpoints}
                />
              </Stack>
            )}
          </Stack>

          {issuerResolved && (
            <Stack gap={4} className="border-t pt-6">
              <SectionHeading
                title="Session Client"
                description="The OAuth client Gram registers and uses with this provider."
              />
              {clientSectionBody}
            </Stack>
          )}

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

// SectionHeading labels each half of the sheet (Identity Provider vs Session
// Client) so the two Select/Add toggles read as distinct steps.
function SectionHeading({
  title,
  description,
}: {
  title: string;
  description: string;
}) {
  return (
    <Stack gap={1}>
      <Label className="text-sm font-medium">{title}</Label>
      <Type muted small>
        {description}
      </Type>
    </Stack>
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
              {issuer.name?.trim() || issuer.slug} — {issuer.issuer}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Type muted small>
        Pick an organization-level or project identity provider already
        configured on this project.
      </Type>
    </Stack>
  );
}

function SelectExistingClientFields({
  clients,
  selectedClientId,
  onChange,
  selectedClient,
}: {
  clients: RemoteSessionClient[];
  selectedClientId: string;
  onChange: (id: string) => void;
  selectedClient: RemoteSessionClient | undefined;
}) {
  return (
    <Stack gap={4}>
      <Stack gap={2}>
        <Label className="text-muted-foreground text-xs">Client</Label>
        <Select value={selectedClientId} onValueChange={onChange}>
          <SelectTrigger>
            <SelectValue placeholder="Choose a client…" />
          </SelectTrigger>
          <SelectContent>
            {clients.map((candidate) => (
              <SelectItem key={candidate.id} value={candidate.id}>
                {remoteSessionClientDisplayName(candidate)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Type muted small>
          Bind an existing client of this provider to this MCP server. The
          client's stored configuration is reused as-is.
        </Type>
      </Stack>

      {selectedClient && <SelectedClientDetails client={selectedClient} />}
    </Stack>
  );
}

// SelectedClientDetails shows the picked client's stored configuration
// read-only — attaching reuses it, so there is nothing to edit here.
function SelectedClientDetails({
  client,
}: {
  client: RemoteSessionClient;
}): JSX.Element {
  let typeValue = "OAuth credentials";
  if (client.clientIdMetadataUri) {
    typeValue = "Client ID Metadata Document (CIMD)";
  }
  const scopeValue =
    client.scope && client.scope.length > 0 ? client.scope.join(", ") : "—";
  const rows: { label: string; value: string; mono: boolean }[] = [
    { label: "Client ID", value: client.clientId, mono: true },
    { label: "Type", value: typeValue, mono: false },
    {
      label: "Token Endpoint Auth Method",
      value: client.tokenEndpointAuthMethod ?? "—",
      mono: false,
    },
    { label: "Scope", value: scopeValue, mono: false },
    { label: "Audience", value: client.audience || "—", mono: false },
  ];
  return (
    <Stack gap={3} className="border-t pt-4">
      {rows.map((row) => (
        <Stack key={row.label} gap={1}>
          <Label className="text-muted-foreground text-xs">{row.label}</Label>
          <Type small mono={row.mono} className="break-all">
            {row.value}
          </Type>
        </Stack>
      ))}
    </Stack>
  );
}
