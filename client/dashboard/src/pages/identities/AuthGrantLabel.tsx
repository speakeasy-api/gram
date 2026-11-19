import { IdentityLink } from "@/components/identity-link";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { mcpServerRouteParam } from "@/lib/sources";
import { useRoutes } from "@/routes";
import { Link } from "react-router";

import type { AuthGrantPart } from "./auth-grant-label";

/**
 * A grant's credential label, with each person linked to their identity page,
 * each upstream OAuth client to its page, and each server to its
 * authentication settings, where the credential lives.
 */
export function AuthGrantLabel({
  parts,
}: {
  parts: AuthGrantPart[];
}): JSX.Element {
  const organization = useOrganization();
  const routes = useRoutes();
  const { orgSlug } = useSlugs();
  const projectSlug = (projectId: string) =>
    organization.projects?.find((project) => project.id === projectId)?.slug;
  return (
    <Text as="span" small>
      {parts.map((part, index) => {
        if (part.kind === "text") return part.text;
        if (part.kind === "person") {
          return (
            <IdentityLink key={index} identifier={{ userId: part.userId }}>
              {part.name}
            </IdentityLink>
          );
        }
        if (part.kind === "upstream") {
          if (!part.issuerId) return part.name;
          return (
            <Link
              key={index}
              to={routes.remoteIdentityProviders.clientDetail.href(
                part.issuerId,
                part.id,
              )}
              className="underline-offset-2 hover:underline"
            >
              {part.name}
            </Link>
          );
        }
        return part.servers.map((server, serverIndex) => {
          const slug = projectSlug(server.projectId);
          const separator = serverIndex > 0 ? ", " : "";
          if (!slug) {
            return `${separator}${server.name}`;
          }
          return (
            <span key={`${index}-${server.id}`}>
              {separator}
              <Link
                to={`/${orgSlug}/projects/${slug}/mcp/${mcpServerRouteParam(server)}/settings#authentication`}
                className="underline-offset-2 hover:underline"
              >
                {server.name}
              </Link>
            </span>
          );
        });
      })}
    </Text>
  );
}
