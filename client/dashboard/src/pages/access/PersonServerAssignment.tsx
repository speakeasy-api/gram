import { Link } from "react-router";
import { useRoutes } from "@/routes";
import { useOrganization } from "@/contexts/Auth";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { RoleLink } from "@/components/role-link";
import { useExplainResourceAccess } from "@gram/client/react-query/explainResourceAccess.js";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { AccessibleMCPServer } from "@gram/client/models/components/accessiblemcpserver.js";
import { effectBadge, directorySourceLabel } from "../mcp/access/explainAccess";
import { narrowingLabel } from "../mcp/access/serverAudience";

/** A person's resolved access, distinct from any one role's grant. */
export function ServerAssignment({
  server,
  member,
}: {
  server: AccessibleMCPServer;
  member: AccessMember;
}): JSX.Element {
  const organization = useOrganization();
  const routes = useRoutes({ projectSlug: server.projectSlug });
  const project = organization.projects?.find(
    (project) => project.id === server.projectId,
  );
  const explanation = useExplainResourceAccess(
    { resourceKind: "mcp", resourceId: server.id, userId: member.id },
    undefined,
    { throwOnError: false },
  );
  const level = explanation.data?.levels.find((level) => level.level === "use");
  const sources = [
    ...new Map(
      (level?.rules ?? [])
        .filter(
          (rule) => rule.effect === "allows" || rule.effect === "overrides",
        )
        .map((rule) => [rule.principalUrn, rule]),
    ).values(),
  ];
  return (
    <div className="border-border space-y-2 border-b py-4">
      <Link
        className="break-words text-sm font-medium hover:underline"
        to={routes.mcp.x.overview.href(server.slug || server.id)}
      >
        {server.name || "Unnamed server"}
      </Link>
      {server.projectSlug !== "default" && (
        <Text small muted>
          {project?.name || server.projectSlug}
        </Text>
      )}
      {explanation.isError && (
        <Button variant="secondary" onClick={() => void explanation.refetch()}>
          Retry loading access
        </Button>
      )}
      {explanation.isPending && (
        <Text small muted>
          Loading access…
        </Text>
      )}
      {level && (
        <details className="text-sm">
          <summary className="cursor-pointer">
            {level.toolAccess === "all" ? "All tools" : "Restricted tools"}
          </summary>
          <div className="space-y-2 py-2">
            {level.rules.map((rule, index) => (
              <div key={index}>
                <Text small>
                  {rule.displayName}: {effectBadge(rule).label.toLowerCase()}{" "}
                  {narrowingLabel(rule)}
                </Text>
                {directorySourceLabel(rule) && (
                  <Text small muted>
                    {directorySourceLabel(rule)}
                  </Text>
                )}
              </div>
            ))}
          </div>
        </details>
      )}
      {sources.length > 0 && (
        <div className="flex flex-wrap gap-1 text-sm">
          <Text small muted>
            Assigned through
          </Text>
          {sources.map((rule, index) => (
            <span key={rule.principalUrn}>
              {index > 0 && ", "}
              {rule.kind === "role" ? (
                <RoleLink principalUrn={rule.principalUrn}>
                  {rule.displayName}
                </RoleLink>
              ) : (
                rule.displayName
              )}
            </span>
          ))}
        </div>
      )}
      {explanation.data?.visibility === "public" && (
        <Text small muted>
          Public server
        </Text>
      )}
    </div>
  );
}
