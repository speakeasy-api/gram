import { cn } from "@/lib/utils";
import { useRBAC } from "@/hooks/useRBAC";
import { useOrgRoutes } from "@/routes";
import { Link } from "react-router";
import { issuerDisplayName } from "./issuerDisplay";

// The subset of a remote identity provider needed to name it and route to it.
// Structural, so the org-scoped and project-scoped issuer models both satisfy it.
type LinkableIssuer = {
  id: string;
  name?: string | null | undefined;
  issuer: string;
};

/**
 * The provider's display name, linked to its detail page.
 *
 * All three tenancy tiers resolve through the tenant-scoped detail page, which
 * renders an inherited platform provider read-only. The platform catalog route
 * is reserved for platform admins.
 *
 * The detail page requires org:read/org:admin; without them the name renders as
 * plain text.
 */
export function IssuerLink({
  issuer,
  className,
}: {
  issuer: LinkableIssuer;
  className?: string;
}): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const { hasAnyScope } = useRBAC();
  const label = issuerDisplayName(issuer);

  if (!hasAnyScope(["org:read", "org:admin"])) {
    return <>{label}</>;
  }

  return (
    <Link
      to={orgRoutes.remoteIdentityProviders.issuerDetail.href(issuer.id)}
      className={cn("hover:text-primary hover:underline", className)}
    >
      {label}
    </Link>
  );
}
