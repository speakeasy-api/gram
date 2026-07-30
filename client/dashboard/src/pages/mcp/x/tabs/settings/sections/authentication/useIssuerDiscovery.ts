import { useSdkClient } from "@/contexts/Sdk";
import { useMutation } from "@tanstack/react-query";
import { useCallback, useMemo, useRef, useState } from "react";
import type { DiscoveredEndpoints } from "./issuerFormUtils";

// Initial form values for the Issuer URL + endpoint fields. Pass `null` for
// the Attach sheet (everything starts blank); pass the loaded record's values
// in the Modify sheet so the snapshot is seeded and the Reset/Discover slot
// behaves consistently against saved state.
export type UseIssuerDiscoveryInitial = {
  issuerUrl: string;
  authorizationEndpoint: string;
  tokenEndpoint: string;
  registrationEndpoint: string;
  jwksUri: string;
  scopesSupported: string[];
  grantTypesSupported: string[];
  responseTypesSupported: string[];
  tokenEndpointAuthMethodsSupported: string[];
  clientIdMetadataDocumentSupported: boolean;
  serviceDocumentation: string;
  opPolicyUri: string;
  opTosUri: string;
} | null;

// Which tier's fetchMetadata endpoint the hook calls. Both return the same
// draft; they differ in what they authorize against.
//
// "project" requires an active project and is right for the per-MCP-server
// authentication sheets, which always operate inside one. "organization"
// requires org:admin and no project at all, which is what the org-admin Remote
// Identity Providers surfaces need: an organization-level issuer has no project
// to authorize against, so calling the project endpoint there authorized
// against whichever project happened to be active in the session.
type UseIssuerDiscoveryScope = "project" | "organization";

// Options controlling how `initial` seeds the hook.
export type UseIssuerDiscoveryOptions = {
  // When false, seed the field values from `initial` but NOT the discovered
  // snapshot, so the Discover control stays available against the seeded URL.
  // The issuer Settings tab uses this to let operators re-run discovery on an
  // already-saved issuer. Defaults to true (Modify sheet behavior: seed the
  // snapshot so Reset works and Discover only appears once the URL changes).
  seedSnapshot?: boolean;
  // Defaults to "project" — the sheets that predate the organization-scoped
  // endpoint all live inside a project.
  scope?: UseIssuerDiscoveryScope;
};

// Encapsulates the Issuer URL + 4 endpoint state, RFC 8414 metadata
// snapshot, discovery RPC call, Discover/Reset slot derivations, and the
// endpoint-validation warnings. Used by both AttachRemoteIdentityProviderSheet
// and ModifyRemoteIdentityProviderSheet — those sheets compose their own
// slug / credentials / override state on top.
function useIssuerDiscoveryImpl(
  initial: UseIssuerDiscoveryInitial,
  options?: UseIssuerDiscoveryOptions,
) {
  const client = useSdkClient();
  const seedSnapshot = options?.seedSnapshot ?? true;
  const scope = options?.scope ?? "project";

  const [issuerUrl, setIssuerUrl] = useState(initial?.issuerUrl ?? "");
  // Tracks the latest Issuer URL so an in-flight discovery's onSuccess can tell
  // whether its result is still relevant — the operator may have edited the URL
  // (or reopened the sheet) while the request was in flight.
  const issuerUrlRef = useRef(issuerUrl);
  issuerUrlRef.current = issuerUrl;
  const [authorizationEndpoint, setAuthorizationEndpoint] = useState(
    initial?.authorizationEndpoint ?? "",
  );
  const [tokenEndpoint, setTokenEndpoint] = useState(
    initial?.tokenEndpoint ?? "",
  );
  const [registrationEndpoint, setRegistrationEndpoint] = useState(
    initial?.registrationEndpoint ?? "",
  );
  const [jwksUri, setJwksUri] = useState(initial?.jwksUri ?? "");
  const [discoveredSnapshot, setDiscoveredSnapshot] =
    useState<DiscoveredEndpoints | null>(() =>
      initial && seedSnapshot
        ? {
            url: initial.issuerUrl,
            authorizationEndpoint: initial.authorizationEndpoint,
            tokenEndpoint: initial.tokenEndpoint,
            registrationEndpoint: initial.registrationEndpoint,
            jwksUri: initial.jwksUri,
            scopesSupported: initial.scopesSupported,
            grantTypesSupported: initial.grantTypesSupported,
            responseTypesSupported: initial.responseTypesSupported,
            tokenEndpointAuthMethodsSupported:
              initial.tokenEndpointAuthMethodsSupported,
            clientIdMetadataDocumentSupported:
              initial.clientIdMetadataDocumentSupported,
            serviceDocumentation: initial.serviceDocumentation,
            opPolicyUri: initial.opPolicyUri,
            opTosUri: initial.opTosUri,
          }
        : null,
    );
  const [discoverRan, setDiscoverRan] = useState(false);

  const discoverMutation = useMutation({
    mutationFn: async (url: string): Promise<DiscoveredEndpoints> => {
      const draft =
        scope === "organization"
          ? await client.organizationRemoteSessionIssuers.fetchMetadata({
              fetchIssuerMetadataRequestBody: { issuer: url },
            })
          : await client.remoteSessionIssuers.fetchMetadata({
              fetchIssuerMetadataRequestBody: { issuer: url },
            });
      return {
        url,
        authorizationEndpoint: draft.authorizationEndpoint ?? "",
        tokenEndpoint: draft.tokenEndpoint ?? "",
        registrationEndpoint: draft.registrationEndpoint ?? "",
        jwksUri: draft.jwksUri ?? "",
        scopesSupported: draft.scopesSupported ?? [],
        grantTypesSupported: draft.grantTypesSupported ?? [],
        responseTypesSupported: draft.responseTypesSupported ?? [],
        tokenEndpointAuthMethodsSupported:
          draft.tokenEndpointAuthMethodsSupported ?? [],
        clientIdMetadataDocumentSupported:
          draft.clientIdMetadataDocumentSupported,
        serviceDocumentation: draft.serviceDocumentation ?? "",
        opPolicyUri: draft.opPolicyUri ?? "",
        opTosUri: draft.opTosUri ?? "",
      };
    },
    onSuccess: (snapshot) => {
      // Discard a stale discovery whose URL no longer matches the field so we
      // never apply one issuer's endpoints to another's URL.
      if (snapshot.url !== issuerUrlRef.current.trim()) return;
      setAuthorizationEndpoint(snapshot.authorizationEndpoint);
      setTokenEndpoint(snapshot.tokenEndpoint);
      setRegistrationEndpoint(snapshot.registrationEndpoint);
      setJwksUri(snapshot.jwksUri);
      setDiscoveredSnapshot(snapshot);
      setDiscoverRan(true);
    },
  });

  const discoverPending = discoverMutation.isPending;
  const discoverError = discoverMutation.error
    ? discoverMutation.error instanceof Error
      ? discoverMutation.error.message
      : "Discovery failed against this issuer URL."
    : null;
  const { reset: resetDiscoverMutation } = discoverMutation;
  const clearDiscoverError = useCallback(() => {
    resetDiscoverMutation();
  }, [resetDiscoverMutation]);

  const urlMatchesLastDiscovery =
    discoveredSnapshot != null && issuerUrl.trim() === discoveredSnapshot.url;
  const showDiscoverControls = !urlMatchesLastDiscovery;

  const endpointsDivergeFromSnapshot = useMemo(() => {
    if (!discoveredSnapshot) return false;
    const fields: ReadonlyArray<readonly [string, string]> = [
      [discoveredSnapshot.authorizationEndpoint, authorizationEndpoint],
      [discoveredSnapshot.tokenEndpoint, tokenEndpoint],
      [discoveredSnapshot.registrationEndpoint, registrationEndpoint],
      [discoveredSnapshot.jwksUri, jwksUri],
    ];
    return fields.some(([snap, current]) => snap !== "" && current !== snap);
  }, [
    discoveredSnapshot,
    authorizationEndpoint,
    tokenEndpoint,
    registrationEndpoint,
    jwksUri,
  ]);

  const showResetControls =
    !showDiscoverControls && endpointsDivergeFromSnapshot;

  const endpointWarnings = useMemo(() => {
    if (!discoverRan) return [];
    const warnings: string[] = [];
    if (!authorizationEndpoint.trim()) {
      warnings.push("Authorization endpoint not advertised by the issuer.");
    }
    if (!tokenEndpoint.trim()) {
      warnings.push("Token endpoint not advertised by the issuer.");
    }
    return warnings;
  }, [discoverRan, authorizationEndpoint, tokenEndpoint]);

  const { mutate: triggerDiscover } = discoverMutation;
  const runDiscover = useCallback(
    (url: string) => {
      const trimmed = url.trim();
      if (!trimmed) return;
      triggerDiscover(trimmed);
    },
    [triggerDiscover],
  );

  // Restore only the fields discovery actually returned. User additions to
  // fields discovery left empty stay untouched.
  const handleResetEndpoints = useCallback(() => {
    if (!discoveredSnapshot) return;
    if (discoveredSnapshot.authorizationEndpoint) {
      setAuthorizationEndpoint(discoveredSnapshot.authorizationEndpoint);
    }
    if (discoveredSnapshot.tokenEndpoint) {
      setTokenEndpoint(discoveredSnapshot.tokenEndpoint);
    }
    if (discoveredSnapshot.registrationEndpoint) {
      setRegistrationEndpoint(discoveredSnapshot.registrationEndpoint);
    }
    if (discoveredSnapshot.jwksUri) {
      setJwksUri(discoveredSnapshot.jwksUri);
    }
  }, [discoveredSnapshot]);

  // Clears endpoint state + snapshot + discoverRan. Used by the URL-change
  // cascade in the parent sheets so a divergent URL leaves the form in a
  // coherent state until the operator re-runs Discover or types values in.
  const resetEndpointState = useCallback(() => {
    setAuthorizationEndpoint("");
    setTokenEndpoint("");
    setRegistrationEndpoint("");
    setJwksUri("");
    setDiscoveredSnapshot(null);
    setDiscoverRan(false);
  }, []);

  // Intentionally narrow: setDiscoveredSnapshot and discoverRan stay
  // internal. Parents drive snapshot transitions via runDiscover /
  // resetEndpointState, and read endpointWarnings rather than discoverRan
  // directly. Keeping these out of the return type prevents callers from
  // bypassing the state machine.
  return {
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
  };
}

export function useIssuerDiscovery(
  initial: UseIssuerDiscoveryInitial,
  options?: UseIssuerDiscoveryOptions,
): ReturnType<typeof useIssuerDiscoveryImpl> {
  return useIssuerDiscoveryImpl(initial, options);
}
