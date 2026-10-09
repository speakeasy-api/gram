import { useOrgRoutes } from "@/routes";
import type { JSX, ReactNode } from "react";
import { Link } from "react-router";

/** A role's id from its principal URN, `role:<tier>:<id>`. */
function roleIdFromPrincipalUrn(principalUrn: string): string | undefined {
  if (!principalUrn.startsWith("role:")) return undefined;
  return principalUrn.split(":").pop() || undefined;
}

/**
 * A role named anywhere in the dashboard, linked to the page that edits it.
 * The rule behind the name lives on the role, not on the page being read, so
 * the fix for what the name is saying is always one click away. Without an id
 * the name is plain text.
 */
export function RoleLink({
  roleId,
  principalUrn,
  children,
}: {
  roleId?: string;
  /** A `role:<tier>:<id>` URN, for surfaces that hold principals. */
  principalUrn?: string;
  children: ReactNode;
}): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const id =
    roleId ?? (principalUrn ? roleIdFromPrincipalUrn(principalUrn) : undefined);
  if (!id) return <>{children}</>;
  return (
    <Link
      to={`${orgRoutes.access.roles.href()}/${encodeURIComponent(id)}/edit`}
      className="underline decoration-dotted underline-offset-4 hover:decoration-solid"
      // Role names sit inside clickable rows and cards; following the link
      // must not also trigger the row.
      onClick={(event) => event.stopPropagation()}
    >
      {children}
    </Link>
  );
}
