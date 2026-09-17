import { Button } from "@/components/ui/Button";
import type { RemoteSessionIssuerDuplicateMatch } from "@gram/client/models/components/remotesessionissuerduplicatematch.js";
import { useOrganizationRemoteSessionIssuers } from "@gram/client/react-query/organizationRemoteSessionIssuers.js";
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
  const { data } = useOrganizationRemoteSessionIssuers(undefined, undefined, {
    enabled: match.tier === "project-specific",
    throwOnError: false,
  });
  const issuer =
    match.tier === "project-specific"
      ? data?.result.items.find((item) => item.issuer.id === match.id)?.issuer
      : { id: match.id };
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
