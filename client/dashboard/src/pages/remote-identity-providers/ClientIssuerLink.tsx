import { useIsPlatformAdmin } from "@/contexts/Auth";
import { remoteSessionScopeTier } from "@/lib/sources";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { IssuerLink } from "./IssuerLink";
import { issuerDisplayName } from "./issuerDisplay";

/**
 * The provider a session client belongs to, linked back to its detail page.
 *
 * A platform provider links only for platform admins; everyone else sees its
 * name as plain text. Other tiers defer to IssuerLink's org scope check.
 */
export function ClientIssuerLink({
  issuer,
}: {
  issuer: RemoteSessionIssuer;
}): JSX.Element {
  const isPlatformAdmin = useIsPlatformAdmin();

  if (remoteSessionScopeTier(issuer) === "platform" && !isPlatformAdmin) {
    return <>{issuerDisplayName(issuer)}</>;
  }

  return <IssuerLink issuer={issuer} className="hover:text-foreground" />;
}
