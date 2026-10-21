import { Alert } from "@/components/ui/Alert";
import { useOrganization } from "@/contexts/Auth";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import { useRoutes } from "@/routes";
import { useDirectoryPeople } from "@gram/client/react-query/directoryPeople.js";
import { Link } from "react-router";

import { NOT_MEMBER_DIRECTORY_STATE_FILTER } from "./directoryPeople";

/**
 * How many people the directory knows who are not members here yet, linking
 * to Identities filtered to exactly those people. Team and the SSO tab show it
 * so a directory that has run ahead of sign-ins is visible where admins manage
 * members. Renders nothing without org:admin (the read names people who are
 * not members), while loading, on error, or when every directory profile
 * already belongs to a member. The link only shows when the viewer can open
 * Identities, which needs project:read.
 */
export function DirectoryPeopleSummary({
  className,
}: {
  className?: string;
}): JSX.Element | null {
  const organization = useOrganization();
  const { hasScope } = useRBAC();
  const canRead = hasScope("org:admin", organization.id);
  // A page of one: the counts cover the whole organization on every page.
  const query = useDirectoryPeople({ limit: 1 }, undefined, {
    enabled: canRead,
    throwOnError: false,
  });
  const projectSlug = useProjectSlugForRequests();
  const routes = useRoutes({ projectSlug });
  const project = organization.projects.find((p) => p.slug === projectSlug);
  const canOpenIdentities =
    project !== undefined && hasScope("project:read", project.id);

  if (!canRead || !query.data) return null;
  const { counts } = query.data;
  const notMembers =
    counts.accountNotMember + counts.notSignedIn + counts.ambiguousEmail;
  if (notMembers === 0) return null;

  const href = `${routes.identities.people.href()}?directory_state=${encodeURIComponent(NOT_MEMBER_DIRECTORY_STATE_FILTER)}`;
  const summary =
    notMembers === 1
      ? "1 person in your directory isn't a member yet."
      : `${notMembers.toLocaleString()} people in your directory aren't members yet.`;

  return (
    <Alert variant="info" dismissible={false} className={className}>
      <span>
        {summary}
        {canOpenIdentities && (
          <>
            {" "}
            <Link to={href} className="underline underline-offset-2">
              View in Identities
            </Link>
          </>
        )}
      </span>
    </Alert>
  );
}
