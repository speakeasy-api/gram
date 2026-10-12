import { Button } from "@/components/ui/Button";
import type { RemoteSessionIssuerDuplicateMatch } from "@gram/client/models/components/remotesessionissuerduplicatematch.js";
import { useOrganizationRemoteSessionIssuer } from "@gram/client/react-query/organizationRemoteSessionIssuer.js";
import { Link } from "react-router";
import { useIssuerHref } from "./useIssuerHref";

export function ExistingIssuerLink({
  match,
  onClick,
}: {
  match: RemoteSessionIssuerDuplicateMatch;
  onClick?: () => void;
}): JSX.Element | null {
  const issuerHref = useIssuerHref();
  // A project-specific issuer's link needs its project, so read the record by
  // id: searching a listing page would miss it once the org has more issuers
  // than fit on one.
  const { data } = useOrganizationRemoteSessionIssuer(
    { id: match.id },
    undefined,
    { enabled: match.tier === "project-specific", throwOnError: false },
  );
  const issuer = match.tier === "project-specific" ? data : { id: match.id };
  const href = issuer && issuerHref(issuer);
  if (!href) return null;
  return (
    <Button asChild variant="secondary">
      <Link to={href} onClick={onClick}>
        View existing provider
      </Link>
    </Button>
  );
}
