import { useOrgRoutes } from "@/routes";
import type { JSX, ReactNode } from "react";
import { Link } from "react-router";

/**
 * A role named on an access surface, linked to the page that edits it. The
 * rule behind the name lives on the role, not on the server being read, so the
 * fix for what the name is saying is always one click away.
 */
export function RoleLink({
  principalUrn,
  children,
}: {
  principalUrn: string;
  children: ReactNode;
}): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const roleId = principalUrn.split(":").pop();
  if (!roleId) return <>{children}</>;
  return (
    <Link
      to={`${orgRoutes.access.roles.href()}/${roleId}/edit`}
      className="underline decoration-dotted underline-offset-4 hover:decoration-solid"
      onClick={(event) => event.stopPropagation()}
    >
      {children}
    </Link>
  );
}
