import { useSdkClient } from "@/contexts/Sdk";
import { useQuery } from "@tanstack/react-query";
import type { RemoteSessionIssuerDuplicateMatch } from "@gram/client/models/components/remotesessionissuerduplicatematch.js";

// Which tier's duplicate preflight to call. Mirrors UseIssuerDiscoveryScope:
// the three endpoints answer the same question and differ only in what they
// authorize against and, consequently, in which records they can see.
//
// "project" sees this project's issuers plus the ones it inherits from the
// organization and the platform catalog. "organization" sees the whole
// organization — project-specific records included, which is the point at that
// tier — plus the platform catalog. "platform" sees the platform catalog only.
export type IssuerDuplicateScope = "project" | "organization" | "platform";

export type { RemoteSessionIssuerDuplicateMatch };

// A URL only reaches the server once it could plausibly be an issuer
// identifier. This exists to avoid spending a request on "h", "ht", "htt", NOT
// to validate: validation stays server-side so it cannot drift from the matching
// rules, and the server already answers an unusable URL with an empty match list.
//
// Deliberately permissive, because anything it rejects is a warning that
// silently never fires. Case-insensitive, since the server lowercases the
// scheme, and it requires only one character of authority so that short hosts
// are not excluded.
function looksLikeIssuerUrl(value: string): boolean {
  return /^https?:\/\/.+/i.test(value.trim());
}

// useIssuerDuplicatePreflight asks whether anything the caller can already see
// describes the issuer URL they are entering, so a create or edit form can warn
// before adding a duplicate.
//
// Deliberately a query keyed on the URL rather than a mutation: the answer is a
// pure function of the URL, so react-query's cache handles both deduplication
// and the stale-response race that useIssuerDiscovery has to guard with a ref.
//
// `enabled` is how callers keep this off a per-keystroke path. These sheets
// make no automatic network calls today — discovery is button-driven — so
// callers pass a URL that has settled (on blur, or alongside Discover) rather
// than the live input value.
// `excludeId` drops one record from the results — the one an edit form is
// editing, which must not be reported as a duplicate of itself. Filtered here
// rather than passed to the server: it only bites for a
// normalization-equivalent self-edit (adding a trailing slash to your own
// URL), since any genuine URL change means the record cannot match itself, and
// a caller that is editing already knows the id.
export function useIssuerDuplicatePreflight({
  issuerUrl,
  scope,
  enabled = true,
  excludeId,
}: {
  issuerUrl: string;
  scope: IssuerDuplicateScope;
  enabled?: boolean;
  excludeId?: string;
}): { matches: RemoteSessionIssuerDuplicateMatch[] } {
  const client = useSdkClient();
  const trimmed = issuerUrl.trim();
  const shouldRun = enabled && looksLikeIssuerUrl(trimmed);

  const query = useQuery({
    queryKey: ["issuer-duplicate-preflight", scope, trimmed],
    enabled: shouldRun,
    // The dashboard's default query policy throws everything but 401/403 to the
    // nearest error boundary. That would let a 500 or a dropped connection on an
    // ADVISORY lookup tear down the form the operator is filling in, which is
    // the opposite of what this feature promises. Opt out: a preflight that
    // cannot answer simply does not warn.
    throwOnError: false,
    queryFn: () => {
      // Returned from the switch rather than assigned in it so the union stays
      // exhaustive: a fourth scope becomes a compile error here instead of
      // silently falling through to the project endpoint.
      switch (scope) {
        case "platform":
          return client.adminRemoteSessions.getGlobalIssuerDuplicatePreflight({
            issuer: trimmed,
          });
        case "organization":
          return client.organizationRemoteSessionIssuers.getDuplicatePreflight({
            issuer: trimmed,
          });
        case "project":
          return client.remoteSessionIssuers.getDuplicatePreflight({
            issuer: trimmed,
          });
      }
    },
  });

  // `shouldRun` gates the RESULT, not just the request. `enabled: false` stops
  // react-query fetching but does not stop it handing back a cached entry for
  // this key, so reading query.data unconditionally would resurrect a warning
  // from an earlier form that happened to look up the same URL — under a field
  // the operator has not touched.
  //
  // A failed preflight likewise surfaces nothing. It is advisory, and a form
  // that shouted because an advisory lookup failed would be worse than one that
  // quietly does not warn.
  // Gated on `shouldRun` because `enabled: false` stops react-query fetching but
  // not reading a cached entry for this key, which would resurrect a warning
  // from an earlier form under a field the operator has not touched. Gated on
  // `error` too, so a failed refetch after a successful lookup does not leave
  // stale advice on screen presented as current.
  const matches = shouldRun && !query.error ? (query.data?.matches ?? []) : [];

  // No pending flag: there is nothing useful to render while an advisory
  // lookup is in flight, and a spinner beside a URL field would imply the form
  // is waiting on it. The warning simply appears once the answer arrives.
  return {
    matches: excludeId
      ? matches.filter((match) => match.id !== excludeId)
      : matches,
  };
}
