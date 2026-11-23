import { AssetImageUploadField } from "@/components/asset-image-upload-field";
import { Dialog } from "@/components/ui/Dialog";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import {
  Sheet,
  SheetContent,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Text } from "@/components/ui/Text";
import { Skeleton } from "@/components/ui/Skeleton";
import { useIsPlatformAdmin } from "@/contexts/Auth";
import { useFetcher } from "@/contexts/Fetcher";
import { useProjectSlugForRequests, useSdkClient } from "@/contexts/Sdk";
import {
  buildUserSessionResourceSlug,
  DEFAULT_USER_SESSION_DURATION_HOURS,
} from "@/lib/externalMcpUserSessions";
import {
  proxyRegisterUpstreamClient,
  registrationProvenance,
} from "@/lib/proxyRegisterUpstreamClient";
import { deriveRemoteSessionIssuerNameFromUrl } from "@/lib/sources";
import { remoteSessionClientDisplayName } from "@/pages/remote-identity-providers/clientDisplay";
import type { CreateRemoteSessionIssuerForm } from "@gram/client/models/components/createremotesessionissuerform.js";
import type { CreateRemoteSessionClientFormTokenEndpointAuthMethod } from "@gram/client/models/components/createremotesessionclientform.js";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { invalidateAllRemoteSessionClients } from "@gram/client/react-query/remoteSessionClients.js";
import { useRemoteSessionIssuer } from "@gram/client/react-query/remoteSessionIssuer.js";
import {
  invalidateAllRemoteSessionIssuers,
  useRemoteSessionIssuers,
} from "@gram/client/react-query/remoteSessionIssuers.js";
import { invalidateAllUserSessionIssuers } from "@gram/client/react-query/userSessionIssuers.js";
import {
  invalidateSharedIssuerImpact,
  needsSharedIssuerConfirm,
  sharedIssuerChangeBlocked,
  useSharedIssuerImpact,
} from "@/lib/remote-identity/model/upstreamRepointing";
import { RepointedServersNotice } from "./RepointedServersNotice";
import { Button } from "@/components/ui/Button";
import { Stack } from "@/components/ui/Stack";
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
  dynamicClientRegistrationAvailability,
  narrowTokenEndpointAuthMethod,
  parseScopes,
  TUNNELED_DCR_PERMISSION_MESSAGE,
} from "./issuerFormUtils";
import {
  copyableAuthMethod,
  pickPreferredAuthMethod,
  usableAsClientTemplate,
} from "@/lib/remote-identity/model/clientConfiguration";
import { IdentityProviderAttachmentErrorAlert } from "./IdentityProviderAttachmentErrorAlert";
import { selectExistingClient } from "./selectExistingClient";
import { useAllRemoteSessionClients } from "@/lib/remote-identity";
import { AuthorizationServerSelect } from "./AuthorizationServerSelect";
import { StartingClientFields } from "./StartingClientFields";
import { useIssuerDiscovery } from "./useIssuerDiscovery";
import { IssuerDuplicateWarning } from "./IssuerDuplicateWarning";
import {
  useIssuerDuplicatePreflight,
  type RemoteSessionIssuerDuplicateMatch,
} from "./useIssuerDuplicatePreflight";
import { CONFIG_WRITE_META } from "@/lib/config-write";

type Mode = "select" | "new";

const NO_EXCLUDED_ISSUERS: readonly string[] = [];
// The listing's server-side page ceiling (constants.MaxPageLimit).
const MAX_ISSUER_PAGE = 100;

export function AttachRemoteIdentityProviderSheet({
  open,
  onOpenChange,
  target,
  userSessionIssuer,
  excludedIssuerIds = NO_EXCLUDED_ISSUERS,
  initialIssuerUrl,
  initialScopes,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  // The MCP server or toolset the issuer gets linked to.
  target: AuthTarget;
  // null when the target has no issuer yet — the first add creates one and
  // links it via target.linkUserSessionIssuer.
  userSessionIssuer: UserSessionIssuer | null;
  // remote_session_issuers already associated with userSessionIssuer, left
  // out of the "Select existing" picker. Every other issuer the project can
  // see (its own, its organization's, the platform catalog's) is offered.
  excludedIssuerIds?: readonly string[];
  // When the caller opens via "Start With Discovered Configuration", this is the
  // authorization_servers[0] entry from the RFC 9728 probe. The sheet starts
  // in "new" mode and runs RFC 8414 discovery against this URL to prefill the
  // upstream endpoints.
  initialIssuerUrl?: string;
  // RFC 9728 protected-resource scopes. Some providers advertise these only
  // on the protected resource, not in authorization-server metadata.
  initialScopes?: string[];
}): JSX.Element {
  const client = useSdkClient();
  const { fetch: authedFetch } = useFetcher();
  const queryClient = useQueryClient();
  const isPlatformAdmin = useIsPlatformAdmin();
  const projectSlug = useProjectSlugForRequests();

  // Whether "Select existing" has anything to offer. One more row than there
  // are exclusions is enough to know: if every row of that page is excluded
  // and no page follows, the project sees no issuer beyond them. The server
  // caps the page, so a following page also counts as something to offer.
  // Fetched while closed too, so the answer is ready when the sheet opens
  // rather than flipping its mode (and resetting the form) a moment later.
  const excludedIds = useMemo(
    () => new Set(excludedIssuerIds),
    [excludedIssuerIds],
  );
  const { data: probePage } = useRemoteSessionIssuers(
    { limit: Math.min(excludedIds.size + 1, MAX_ISSUER_PAGE) },
    undefined,
    { throwOnError: false },
  );
  const hasSelectable =
    !!probePage?.result.nextCursor ||
    (probePage?.result.items ?? []).some(
      (issuer) => !excludedIds.has(issuer.id),
    );
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

  // Optional logo, uploaded ahead of submit; empty submits as undefined so the
  // backend stores NULL. Submit is held while an upload is in flight so a
  // mid-upload submit cannot silently drop the picked logo.
  const [logoAssetId, setLogoAssetId] = useState("");
  const [logoUploading, setLogoUploading] = useState(false);

  // The Issuer URL as it stood when the operator last left the field. Held
  // separately from the live input so the duplicate preflight runs on a settled
  // value rather than once per keystroke.
  const [settledIssuerUrl, setSettledIssuerUrl] = useState("");
  // Project scope: this project's own issuers plus the ones it inherits from
  // the organization and the platform catalog. Only the "new" arm can create a
  // duplicate — "select" mode is already reuse — so the lookup is off there.
  const { matches: duplicateMatches } = useIssuerDuplicatePreflight({
    issuerUrl: settledIssuerUrl,
    scope: "project",
    enabled: open && mode === "new",
  });

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

  // Session Client section state. Either way the sheet creates a client of
  // the target's own: "select" starts it from a stored client's settings and
  // never binds that client, because a person holds one upstream grant per
  // client and sharing one lets a sign-in on one server replace another's.
  // clientType drives how a from-scratch client is created (DCR / CIMD /
  // Manual); it is reset to the recommended default in an effect below.
  const [clientMode, setClientMode] = useState<Mode>("select");
  const [selectedClientId, setSelectedClientId] = useState("");
  const [clientType, setClientType] = useState<ClientType>("manual");

  // Resolve the issuer record the operator picked in Select-existing mode. We
  // need it to know whether the picked issuer supports DCR/CIMD and to pull its
  // registration_endpoint at submit time. Fetched by id: the picker pages and
  // searches the listing, so the pick may not be in any page it has loaded.
  const { data: pickedIssuer, isError: pickedIssuerError } =
    useRemoteSessionIssuer({ id: selectedIssuerId }, undefined, {
      enabled: mode === "select" && !!selectedIssuerId,
      throwOnError: false,
    });
  const selectedIssuer =
    mode === "select" && pickedIssuer?.id === selectedIssuerId
      ? pickedIssuer
      : undefined;

  // DCR and CIMD availability drive the Client Type selector. In Add-new the
  // values come from the form (filled by discovery or typed); in
  // Select-existing they come from the picked issuer record.
  const registrationEndpointForClient =
    mode === "new"
      ? registrationEndpoint
      : selectedIssuer?.registrationEndpoint;
  const {
    available: dcrAvailable,
    permissionRestricted: tunneledDcrRestricted,
  } = dynamicClientRegistrationAvailability({
    registrationEndpoint: registrationEndpointForClient,
    tunneled: !!selectedIssuer?.tunneledMcpServerId,
    isPlatformAdmin,
  });
  const cimdAvailable =
    mode === "new"
      ? (discoveredSnapshot?.clientIdMetadataDocumentSupported ?? false)
      : !!selectedIssuer?.clientIdMetadataDocumentSupported;
  const clientTypes = useMemo(
    () => availableClientTypes({ dcrAvailable, cimdAvailable }),
    [dcrAvailable, cimdAvailable],
  );

  // Clients of the picked issuer a new one can start from (this project's
  // clients, whether the issuer is organization-level or project-level). Only
  // an existing issuer can have clients, so the walk is disabled in
  // Add-new-issuer mode. Leaves out any already bound to this
  // user_session_issuer, which keeps one client per issuer.
  const { items: issuerClients, isLoading: isLoadingIssuerClients } =
    useAllRemoteSessionClients(
      { remoteSessionIssuerId: selectedIssuerId },
      { enabled: mode === "select" && !!selectedIssuerId },
    );
  const attachableClients = useMemo(
    () =>
      issuerClients.filter(
        (candidate) =>
          usableAsClientTemplate(candidate, cimdAvailable) &&
          (!userSessionIssuer ||
            !candidate.userSessionIssuerIds.includes(userSessionIssuer.id)),
      ),
    [issuerClients, userSessionIssuer, cimdAvailable],
  );

  // The Session Client toggle only appears for an existing issuer that has a
  // client to start from; otherwise the sheet shows the from-scratch form
  // directly (a brand-new issuer never has clients).
  const clientToggleVisible = attachableClients.length > 0;
  const effectiveClientMode: Mode = clientToggleVisible ? clientMode : "new";
  const selectedClient = selectExistingClient(
    attachableClients,
    selectedClientId,
    !isLoadingIssuerClients,
  );
  const effectiveSelectedClientId = selectedClient?.id ?? "";
  // The Session Client section stays hidden until an identity provider is
  // determined — an existing one is picked, or a new one's Issuer URL has been
  // entered — since every client choice depends on that provider.
  const issuerResolved =
    mode === "select" ? !!selectedIssuerId : issuerUrl.trim().length > 0;

  const attachMutation = useMutation({
    meta: CONFIG_WRITE_META,
    mutationFn: async (): Promise<{
      unsupportedDcrAuthMethod: string | null;
    }> => {
      const parsedScopes = parseScopes(scopeOverride);
      const trimmedAudience = audienceOverride.trim();
      let createProvider: CreateRemoteSessionIssuerForm | undefined;
      if (mode === "new") {
        createProvider = {
          slug: slug.trim(),
          issuer: issuerUrl.trim(),
          name: name.trim() || undefined,
          logoAssetId: logoAssetId || undefined,
          authorizationEndpoint: authorizationEndpoint.trim() || undefined,
          tokenEndpoint: tokenEndpoint.trim() || undefined,
          registrationEndpoint: registrationEndpoint.trim() || undefined,
          jwksUri: jwksUri.trim() || undefined,
          scopesSupported: discoveredSnapshot?.scopesSupported ?? [],
          grantTypesSupported: discoveredSnapshot?.grantTypesSupported ?? [],
          // Omitted without discovery: the server treats any value as discovered.
          authorizationGrantProfilesSupported:
            discoveredSnapshot?.authorizationGrantProfilesSupported,
          responseTypesSupported:
            discoveredSnapshot?.responseTypesSupported ?? [],
          tokenEndpointAuthMethodsSupported:
            discoveredSnapshot?.tokenEndpointAuthMethodsSupported ?? [],
          codeChallengeMethodsSupported:
            discoveredSnapshot?.codeChallengeMethodsSupported ?? undefined,
          clientIdMetadataDocumentSupported:
            discoveredSnapshot?.clientIdMetadataDocumentSupported ?? false,
          revocationEndpoint:
            discoveredSnapshot?.revocationEndpoint || undefined,
          serviceDocumentation:
            discoveredSnapshot?.serviceDocumentation || undefined,
          opPolicyUri: discoveredSnapshot?.opPolicyUri || undefined,
          opTosUri: discoveredSnapshot?.opTosUri || undefined,
          userinfoEndpoint: discoveredSnapshot?.userinfoEndpoint || undefined,
          introspectionEndpoint:
            discoveredSnapshot?.introspectionEndpoint || undefined,
          introspectionEndpointAuthMethodsSupported:
            discoveredSnapshot?.introspectionEndpointAuthMethodsSupported ??
            undefined,
          idTokenSigningAlgValuesSupported:
            discoveredSnapshot?.idTokenSigningAlgValuesSupported ?? undefined,
          claimsSupported: discoveredSnapshot?.claimsSupported ?? undefined,
          backchannelLogoutSupported:
            discoveredSnapshot?.backchannelLogoutSupported ?? undefined,
          authorizationResponseIssParameterSupported:
            discoveredSnapshot?.authorizationResponseIssParameterSupported ??
            undefined,
        };
      }

      // Step 1: ensure a user_session_issuer exists. First-add auto-creates
      // one with the conservative interactive challenge mode and a 2-week
      // session lifetime.
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
        resolvedIssuer = selectedIssuer;
      } else {
        const created = await client.remoteSessionIssuers.create({
          createRemoteSessionIssuerForm: createProvider!,
        });
        remoteIssuerId = created.id;
        resolvedIssuer = created;
      }

      // Step 3: create a remote_session_client bound to the
      // user_session_issuer: from scratch in the chosen mode (DCR / CIMD /
      // Manual), or from a stored client's reviewed settings. A stored client
      // is never bound here.
      // Tracks when DCR returns an auth method the SDK enum doesn't model
      // (e.g. `private_key_jwt`). We swallow it for the local client record
      // but warn the operator after success so they understand why their
      // client may not behave as the upstream advertised.
      let unsupportedDcrAuthMethod: string | null = null;

      if (effectiveClientMode === "select" && !selectedClient) {
        throw new Error("Choose the client to start from.");
      }
      const startsFromDocument =
        effectiveClientMode === "select" &&
        !!selectedClient?.clientIdMetadataUri;
      if (
        effectiveClientMode === "select" &&
        selectedClient &&
        !startsFromDocument
      ) {
        // A new client on the stored one's registration. Naming it lets the
        // server copy the secret and callback; the secret never reaches the
        // browser. A different client ID is a different registration, so
        // nothing is copied for it.
        await client.remoteSessionClients.create({
          createRemoteSessionClientForm: {
            remoteSessionIssuerId: remoteIssuerId,
            userSessionIssuerIds: [issuerId],
            clientId: clientId.trim(),
            clientSecret: clientSecret.trim() || undefined,
            templateClientId:
              clientId.trim() === selectedClient.clientId
                ? selectedClient.id
                : undefined,
            tokenEndpointAuthMethod: tokenEndpointAuthMethod || undefined,
            scope: parsedScopes.length > 0 ? parsedScopes : undefined,
            audience: trimmedAudience || undefined,
          },
        });
      } else if (startsFromDocument || clientType === "cimd") {
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
          // Set for DCR so the server records when the issuer expires the
          // client and can re-register it in place.
          provenance?: ReturnType<typeof registrationProvenance>;
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
            tunneledMcpServerId: resolvedIssuer?.tunneledMcpServerId,
            projectSlug,
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
            provenance: registrationProvenance(registered),
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
            ...clientCredentials.provenance,
          },
        });
      }

      // Step 4: on first-add, link the target to the new issuer. How the link
      // is stored (and side effects like flipping a server private) is the
      // target's business.
      if (!userSessionIssuer) {
        await target.linkUserSessionIssuer?.(issuerId);
      }

      return { unsupportedDcrAuthMethod };
    },
    onSuccess: async ({ unsupportedDcrAuthMethod }) => {
      await Promise.all([
        invalidateAllUserSessionIssuers(queryClient, { refetchType: "all" }),
        invalidateAllRemoteSessionIssuers(queryClient, { refetchType: "all" }),
        invalidateAllRemoteSessionClients(queryClient, { refetchType: "all" }),
        invalidateSharedIssuerImpact(queryClient),
        target.invalidate(queryClient),
      ]);

      toast.success("Authorization server attached");
      if (unsupportedDcrAuthMethod) {
        toast.warning(
          `Upstream issuer reported token endpoint auth method "${unsupportedDcrAuthMethod}", which the platform doesn't model. The client falls back to ${tokenEndpointAuthMethod || "client_secret_basic"} — adjust on the authorization server's Modify sheet if needed.`,
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
      console.error("Attach authorization server failed", error);
    },
  });

  const submitting = attachMutation.isPending;
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
    setLogoAssetId("");
    setIssuerUrl(initialIssuerUrl ?? "");
    resetEndpointState();
    clearDiscoverError();
    // Seeded, not cleared: "Start With Discovered Configuration" opens this
    // sheet in "new" mode with the URL already filled in and runs discovery
    // automatically. That is the path most likely to duplicate a platform or
    // organization record, so it has to preflight without waiting for a blur on
    // a field the operator has no reason to touch.
    setSettledIssuerUrl(initialIssuerUrl ?? "");
    setClientId("");
    setClientSecret("");
    setTokenEndpointAuthMethod("");
    setScopeOverride(initialScopes?.join(", ") ?? "");
    setAudienceOverride("");
    setClientMode("select");
    setSelectedClientId("");
    resetAttachMutation();
  }, [
    open,
    target.slug,
    initialIssuerUrl,
    initialScopes,
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
    setClientType(clientTypes[0] ?? "manual");
  }, [clientTypes]);

  // A different issuer (or switching to Add-new issuer) means a different
  // client list; drop any stale existing-client selection.
  useEffect(() => {
    setSelectedClientId("");
  }, [selectedIssuerId, mode]);

  // The client fields follow the client the new one starts from: filled from
  // it for review, and emptied when the issuer changes or it goes away, so a
  // previous registration's settings never stay behind as if typed. Only what
  // the listing returns is copied here; the secret field stays blank.
  const startingClient =
    effectiveClientMode === "select" ? selectedClient : undefined;
  const startingClientKey = startingClient?.id ?? "";
  useEffect(() => {
    setClientId(
      !startingClient || startingClient.clientIdMetadataUri
        ? ""
        : startingClient.clientId,
    );
    setClientSecret("");
    setTokenEndpointAuthMethod(
      copyableAuthMethod(startingClient?.tokenEndpointAuthMethod) ?? "",
    );
    setScopeOverride(
      (startingClient ? startingClient.scope : initialScopes)?.join(", ") ?? "",
    );
    setAudienceOverride(startingClient?.audience ?? "");
    // Keyed by the issuer and the pick alone: a refetch of the same client
    // must not discard the operator's edits.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedIssuerId, mode, startingClientKey]);

  // Leaving a starting client behind must not leave its settings posing as
  // typed ones.
  const changeClientMode = (next: Mode) => {
    if (next === clientMode) return;
    setClientMode(next);
    setSelectedClientId("");
    setClientId("");
    setClientSecret("");
    setTokenEndpointAuthMethod("");
    setScopeOverride(initialScopes?.join(", ") ?? "");
    setAudienceOverride("");
  };

  // One-click reuse switches this sheet from creating an issuer to selecting
  // the one that already describes the URL. It is not offered when the match
  // is already attached to this target: the warning text alone is the right
  // outcome there, since there is nothing left to reuse.
  const primaryDuplicate = duplicateMatches[0];
  const canReuseDuplicate =
    primaryDuplicate !== undefined && !excludedIds.has(primaryDuplicate.id);
  const handleUseExistingIssuer = (
    match: RemoteSessionIssuerDuplicateMatch,
  ) => {
    setSelectedIssuerId(match.id);
    setMode("select");
  };

  const submittable = useMemo(() => {
    // Issuer must be resolvable: an existing pick, or a complete new-issuer
    // form.
    if (mode === "select") {
      // Held until the pick's record loads: submit needs its registration
      // metadata, and a failed lookup must not attach without it.
      if (!selectedIssuer) return false;
    } else if (!slug.trim() || !issuerUrl.trim()) {
      return false;
    }
    // Session client: a reviewed copy of a stored one, or the from-scratch
    // form. A copy of a hosted document needs no client ID of its own.
    if (effectiveClientMode === "select") {
      if (!selectedClient) return false;
      return !!selectedClient.clientIdMetadataUri || !!clientId.trim();
    }
    // Manual requires a client_id; DCR mints one; CIMD needs none.
    if (!clientTypes.includes(clientType)) return false;
    if (clientType === "manual" && !clientId.trim()) return false;
    return true;
  }, [
    mode,
    selectedIssuer,
    slug,
    issuerUrl,
    effectiveClientMode,
    selectedClient,
    clientType,
    clientTypes,
    clientId,
  ]);

  // Linking another provider changes the upstream derived for every server
  // sharing this user session issuer, in any project for an organization one.
  const sharedIssuerId = userSessionIssuer?.id;
  const sharedIssuerImpact = useSharedIssuerImpact(
    open && sharedIssuerId && submittable
      ? {
          userSessionIssuerId: sharedIssuerId,
          mcpServerId: target.mcpServerId,
          change: "attach",
          providerId: mode === "select" ? selectedIssuerId : undefined,
        }
      : null,
  );
  const repointLookupPending = sharedIssuerImpact.pending;
  const [confirmRepointOpen, setConfirmRepointOpen] = useState(false);
  useEffect(() => {
    setConfirmRepointOpen(false);
  }, [open]);

  const handleSubmit = () => {
    if (!submittable || submitting || logoUploading) return;
    if (needsSharedIssuerConfirm(sharedIssuerImpact)) {
      setConfirmRepointOpen(true);
      return;
    }
    attachMutation.mutate();
  };

  // The Session Client section body: its loading, select-existing, and add-new
  // branches, kept out of the JSX so the render stays flat.
  const clientToggle = clientToggleVisible ? (
    <ModeSwitch
      mode={clientMode}
      onChange={changeClientMode}
      selectLabel="New from existing"
      newLabel="New from scratch"
    />
  ) : null;
  let clientSectionBody: JSX.Element;
  if (mode === "select" && selectedIssuerId && isLoadingIssuerClients) {
    clientSectionBody = (
      <Skeleton label="Loading clients" className="h-9 w-full" />
    );
  } else if (effectiveClientMode === "select") {
    clientSectionBody = (
      <Stack gap={4}>
        {clientToggle}
        <SelectExistingClientFields
          clients={attachableClients}
          selectedClientId={effectiveSelectedClientId}
          onChange={setSelectedClientId}
        />
        {selectedClient ? (
          <>
            <StartingClientFields
              starting={selectedClient}
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
          </>
        ) : null}
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
        {tunneledDcrRestricted && (
          <Text muted small>
            {TUNNELED_DCR_PERMISSION_MESSAGE}
          </Text>
        )}
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
    <>
      <Sheet open={open} onOpenChange={onOpenChange}>
        <SheetContent
          side="right"
          className="flex w-[560px] flex-col sm:max-w-[560px]"
        >
          <SheetHeader className="px-6 pt-6 pb-0">
            <SheetTitle className="text-lg font-semibold">
              Attach Authorization Server
            </SheetTitle>
          </SheetHeader>

          <div className="flex-1 space-y-6 overflow-y-auto px-6 py-6">
            <Stack gap={4}>
              <SectionHeading
                title="Authorization server"
                description="The upstream OAuth authorization server Speakeasy delegates to."
              />
              {hasSelectable && <ModeSwitch mode={mode} onChange={setMode} />}

              {mode === "select" ? (
                <AuthorizationServerSelect
                  excludedIssuerIds={excludedIds}
                  selectedIssuerId={selectedIssuerId}
                  selectedIssuer={selectedIssuer}
                  pickFailed={pickedIssuerError}
                  onChange={setSelectedIssuerId}
                />
              ) : (
                <Stack gap={4}>
                  <IssuerUrlField
                    issuerUrl={issuerUrl}
                    onIssuerUrlSettled={setSettledIssuerUrl}
                    duplicateWarning={
                      <IssuerDuplicateWarning
                        viewerScope="project"
                        matches={duplicateMatches}
                        onUseExisting={
                          canReuseDuplicate
                            ? handleUseExistingIssuer
                            : undefined
                        }
                      />
                    }
                    onIssuerUrlChange={(value) => {
                      setIssuerUrl(value);
                      // Any edit invalidates the last blur, so the warning cannot
                      // outlive the URL it describes and "Use existing" cannot
                      // adopt a record for a URL no longer in the field.
                      setSettledIssuerUrl("");
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
                    <Label className="text-muted-foreground text-xs">
                      Slug
                    </Label>
                    <Input
                      value={slug}
                      onChange={(value) => {
                        setSlug(value);
                        setSlugDirty(true);
                      }}
                      placeholder="my-authorization-server"
                    />
                    <Text muted small>
                      Project-unique identifier for this authorization server.
                      Auto-derived from the Issuer URL until you edit it.
                    </Text>
                  </Stack>

                  <Stack gap={2}>
                    <Label className="text-muted-foreground text-xs">
                      Display name (optional)
                    </Label>
                    <Input
                      value={name}
                      onChange={(value) => {
                        setName(value);
                        setNameDirty(true);
                      }}
                      placeholder="My Authorization Server"
                    />
                    <Text muted small>
                      Friendly label shown in the dashboard. Auto-derived from
                      the Issuer URL until you edit it; falls back to the Issuer
                      URL when left blank.
                    </Text>
                  </Stack>

                  <AssetImageUploadField
                    tier="project"
                    value={logoAssetId}
                    onChange={setLogoAssetId}
                    onUploadingChange={setLogoUploading}
                    description="Shown beside this authorization server in the dashboard and on the connect consent page."
                  />

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
                  title="Session client"
                  description="The OAuth client Speakeasy registers and uses with this authorization server."
                />
                {clientSectionBody}
              </Stack>
            )}

            <IdentityProviderAttachmentErrorAlert
              error={attachMutation.error}
            />
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
              disabled={
                !submittable ||
                submitting ||
                logoUploading ||
                repointLookupPending
              }
              onClick={handleSubmit}
            >
              <Button.Text>
                {submitting ? "Attaching…" : "Attach Authorization Server"}
              </Button.Text>
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>
      <Dialog open={confirmRepointOpen} onOpenChange={setConfirmRepointOpen}>
        <Dialog.Content className="max-w-md">
          <Dialog.Header>
            <Dialog.Title>Change the upstream of other servers?</Dialog.Title>
            <Dialog.Description className="sr-only">
              Attaching changes the upstream of other servers.
            </Dialog.Description>
          </Dialog.Header>
          <RepointedServersNotice
            impact={sharedIssuerImpact}
            projectId={target.projectId}
          />
          <Dialog.Footer>
            <Button
              variant="secondary"
              onClick={() => setConfirmRepointOpen(false)}
            >
              <Button.Text>Cancel</Button.Text>
            </Button>
            <Button
              variant="destructive-primary"
              disabled={
                submitting ||
                repointLookupPending ||
                sharedIssuerChangeBlocked(sharedIssuerImpact)
              }
              onClick={() => {
                setConfirmRepointOpen(false);
                attachMutation.mutate();
              }}
            >
              <Button.Text>Attach Authorization Server</Button.Text>
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </>
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
      <Text muted small>
        {description}
      </Text>
    </Stack>
  );
}

function ModeSwitch({
  mode,
  onChange,
  selectLabel = "Select existing",
  newLabel = "Add new",
}: {
  mode: Mode;
  onChange: (next: Mode) => void;
  selectLabel?: string;
  newLabel?: string;
}) {
  return (
    <Stack direction="horizontal" gap={2}>
      <Button
        variant={mode === "select" ? "primary" : "secondary"}
        onClick={() => onChange("select")}
      >
        <Button.Text>{selectLabel}</Button.Text>
      </Button>
      <Button
        variant={mode === "new" ? "primary" : "secondary"}
        onClick={() => onChange("new")}
      >
        <Button.Text>{newLabel}</Button.Text>
      </Button>
    </Stack>
  );
}

// SelectExistingClientFields picks the stored client a new one starts from.
// The pick is a starting point only: its settings fill the form below for
// review, and the client itself is never bound to this target.
function SelectExistingClientFields({
  clients,
  selectedClientId,
  onChange,
}: {
  clients: RemoteSessionClient[];
  selectedClientId: string;
  onChange: (id: string) => void;
}) {
  return (
    <Stack gap={2}>
      <Label className="text-muted-foreground text-xs">Start from</Label>
      <Select value={selectedClientId} onValueChange={onChange}>
        <SelectTrigger aria-label="Start from">
          <SelectValue placeholder="Choose a client…" />
        </SelectTrigger>
        <SelectContent>
          {clients.map((candidate) => (
            <SelectItem key={candidate.id} value={candidate.id}>
              {remoteSessionClientDisplayName(candidate)}
              {/* Names aren't unique, so keep the ID that tells two apart. */}
              {candidate.displayName && (
                <span className="text-muted-foreground ml-2 font-mono text-xs">
                  {candidate.clientId}
                </span>
              )}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Text muted small>
        Attaching creates a separate client with the settings below. The client
        you start from, the servers that use it, and the people signed in
        through it stay as they are.
      </Text>
    </Stack>
  );
}
