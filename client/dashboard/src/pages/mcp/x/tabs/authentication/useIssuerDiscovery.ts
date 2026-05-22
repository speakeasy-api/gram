import { useSdkClient } from "@/contexts/Sdk";
import { useCallback, useMemo, useState } from "react";
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
} | null;

// Encapsulates the Issuer URL + 4 endpoint state, RFC 8414 metadata
// snapshot, discovery RPC call, Discover/Reset slot derivations, and the
// endpoint-validation warnings. Used by both AttachRemoteIdentityProviderSheet
// and ModifyRemoteIdentityProviderSheet — those sheets compose their own
// slug / credentials / override state on top.
export function useIssuerDiscovery(initial: UseIssuerDiscoveryInitial) {
  const client = useSdkClient();

  const [issuerUrl, setIssuerUrl] = useState(initial?.issuerUrl ?? "");
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
      initial
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
          }
        : null,
    );
  const [discoverPending, setDiscoverPending] = useState(false);
  const [discoverError, setDiscoverError] = useState<string | null>(null);
  const [discoverRan, setDiscoverRan] = useState(false);

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

  const runDiscover = useCallback(
    async (url: string) => {
      const trimmed = url.trim();
      if (!trimmed) return;
      setDiscoverPending(true);
      setDiscoverError(null);
      try {
        const draft = await client.remoteSessionIssuers.discover({
          discoverRemoteSessionIssuerRequestBody: { issuer: trimmed },
        });
        const snapshot: DiscoveredEndpoints = {
          url: trimmed,
          authorizationEndpoint: draft.authorizationEndpoint ?? "",
          tokenEndpoint: draft.tokenEndpoint ?? "",
          registrationEndpoint: draft.registrationEndpoint ?? "",
          jwksUri: draft.jwksUri ?? "",
          scopesSupported: draft.scopesSupported ?? [],
          grantTypesSupported: draft.grantTypesSupported ?? [],
          responseTypesSupported: draft.responseTypesSupported ?? [],
          tokenEndpointAuthMethodsSupported:
            draft.tokenEndpointAuthMethodsSupported ?? [],
        };
        setAuthorizationEndpoint(snapshot.authorizationEndpoint);
        setTokenEndpoint(snapshot.tokenEndpoint);
        setRegistrationEndpoint(snapshot.registrationEndpoint);
        setJwksUri(snapshot.jwksUri);
        setDiscoveredSnapshot(snapshot);
        setDiscoverRan(true);
      } catch (error) {
        setDiscoverError(
          error instanceof Error
            ? error.message
            : "Discovery failed against this issuer URL.",
        );
      } finally {
        setDiscoverPending(false);
      }
    },
    [client],
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
    setDiscoverError,
    runDiscover,
    handleResetEndpoints,
    resetEndpointState,
    showDiscoverControls,
    showResetControls,
    endpointWarnings,
  };
}
