import { ErrorAlert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useDrainInfiniteQuery } from "@/hooks/useDrainInfiniteQuery";
import { usePluginQueryScope } from "@/pages/plugins/usePluginQueryScope";
import { usePluginServerQueries } from "@/pages/plugins/usePluginServerQueries";
import { useOrgRoutes, useRoutes } from "@/routes";
import type { SkillLink } from "@gram/client/models/components/skilllink.js";
import { useLinkSkillMutation } from "@gram/client/react-query/linkSkill.js";
import {
  invalidateAllSkillLinks,
  useSkillLinksInfinite,
} from "@gram/client/react-query/skillLinks.js";
import { useUnlinkSkillMutation } from "@gram/client/react-query/unlinkSkill.js";
import { useQueryClient } from "@tanstack/react-query";
import { Network, Trash2 } from "lucide-react";
import { useMemo } from "react";
import { Link } from "react-router";
import { toast } from "sonner";
import { useCanWriteSkillLinkTarget } from "./use-skill-link-write-access";
import { SkillDistributionVersionBadge } from "./skill-badges";
import {
  type SkillLinkTarget,
  skillLinkTargetKindLabel,
  skillLinkTargetName,
  sortSkillLinks,
} from "./skill-links";

const ORGANIZATION_GATEWAY: SkillLinkTarget = { kind: "organization_gateway" };

/**
 * The servers and gateways that serve one skill over MCP, read from the
 * skill's links. Each row opens the place where its skills are managed.
 * Servers and gateways add skills from their own Skills tab; the
 * organization gateway has no project page, so it is added from here.
 */
export function SkillServedOverMcpSection({
  skillId,
  skillDisplayName,
}: {
  skillId: string;
  skillDisplayName: string;
}): JSX.Element {
  const scope = usePluginQueryScope();
  const queryClient = useQueryClient();
  const linksQuery = useSkillLinksInfinite(
    { ...scope, skillId, limit: 50 },
    undefined,
    { throwOnError: false },
  );
  useDrainInfiniteQuery(linksQuery);
  const links = useMemo(
    () =>
      sortSkillLinks(
        linksQuery.data?.pages.flatMap((page) => page.result.links) ?? [],
      ),
    [linksQuery.data?.pages],
  );
  const isLoaded = !!linksQuery.data && !linksQuery.hasNextPage;

  // Toolsets resolve a server link to its page and to the toolset its MCP
  // grants are keyed on; readers without MCP read see names without links.
  const { toolsetsQuery, serversQuery } = usePluginServerQueries();
  const toolsetSlugs = useMemo(
    () =>
      new Map(
        (toolsetsQuery.data?.toolsets ?? []).map((toolset) => [
          toolset.id,
          toolset.slug,
        ]),
      ),
    [toolsetsQuery.data?.toolsets],
  );
  const serverToolsets = useMemo(
    () =>
      new Map(
        (serversQuery.data?.mcpServers ?? []).map((server) => [
          server.id,
          server.toolsetId,
        ]),
      ),
    [serversQuery.data?.mcpServers],
  );

  const canWriteOrgGateway = useCanWriteSkillLinkTarget(ORGANIZATION_GATEWAY);
  const hasOrgGatewayLink = links.some(
    (link) => link.targetKind === "organization_gateway",
  );
  const link = useLinkSkillMutation();
  const handleServeThroughOrgGateway = () => {
    link.mutate(
      {
        request: {
          linkSkillRequestBody: {
            id: skillId,
            targetKind: "organization_gateway",
          },
        },
      },
      {
        onSuccess: () => {
          void invalidateAllSkillLinks(queryClient);
          toast.success(
            `${skillDisplayName} added to the organization gateway`,
          );
        },
        onError: () => {
          toast.error("Unable to add skill to the organization gateway");
        },
      },
    );
  };

  const unlink = useUnlinkSkillMutation();
  const handleRemove = (row: SkillLink) => {
    unlink.mutate(
      { request: { unlinkSkillRequestBody: { linkId: row.id } } },
      {
        onSuccess: () => {
          void invalidateAllSkillLinks(queryClient);
          toast.success(`Removed from ${skillLinkTargetName(row)}`);
        },
        onError: () => {
          toast.error("Unable to remove skill");
        },
      },
    );
  };

  if (linksQuery.error) {
    return (
      <ErrorAlert
        title="Unable to load where this skill is served"
        error={linksQuery.error}
      />
    );
  }
  if (!isLoaded) {
    return <Skeleton className="h-24 w-full" />;
  }

  const orgGatewayControl =
    canWriteOrgGateway && !hasOrgGatewayLink ? (
      <Button
        type="button"
        variant="secondary"
        size="sm"
        disabled={link.isPending}
        onClick={handleServeThroughOrgGateway}
      >
        <Button.LeftIcon>
          <Network className="h-4 w-4" />
        </Button.LeftIcon>
        <Button.Text>Add to organization gateway</Button.Text>
      </Button>
    ) : null;

  return (
    <div className="space-y-3">
      {links.length === 0 ? (
        <div className="border-border border border-dashed p-6">
          <Text small muted>
            Not served over MCP yet. Add it from the Skills tab of an MCP server
            or gateway
            {orgGatewayControl ? ", or to the organization gateway." : "."}
          </Text>
        </div>
      ) : (
        <ul className="border-border bg-card divide-y overflow-hidden border">
          {links.map((row) => (
            <ServedByRow
              key={row.id}
              link={row}
              toolsetSlugs={toolsetSlugs}
              serverToolsets={serverToolsets}
              isRemoving={unlink.isPending}
              onRemove={() => handleRemove(row)}
            />
          ))}
        </ul>
      )}
      {orgGatewayControl}
    </div>
  );
}

function ServedByRow({
  link,
  toolsetSlugs,
  serverToolsets,
  isRemoving,
  onRemove,
}: {
  link: SkillLink;
  toolsetSlugs: ReadonlyMap<string, string>;
  serverToolsets: ReadonlyMap<string, string | undefined>;
  isRemoving: boolean;
  onRemove: () => void;
}): JSX.Element {
  const routes = useRoutes();
  const orgRoutes = useOrgRoutes();
  const target = linkTarget(link);
  // A toolset's own server shares the toolset id; another server built on a
  // toolset carries it in the server list.
  const toolsetId = link.mcpServerId
    ? (serverToolsets.get(link.mcpServerId) ?? link.mcpServerId)
    : undefined;
  const canRemove = useCanWriteSkillLinkTarget(target, {
    grantResourceId: toolsetId,
  });
  const name = skillLinkTargetName(link);

  let href: string | undefined;
  switch (link.targetKind) {
    case "mcp_server": {
      // Only the toolset's own server has a Skills tab that lists this link.
      const slug =
        link.mcpServerId && toolsetId === link.mcpServerId
          ? toolsetSlugs.get(link.mcpServerId)
          : undefined;
      href = slug ? routes.mcp.details.skills.href(slug) : undefined;
      break;
    }
    case "meta_mcp_server":
      href = link.metaMcpServerId
        ? routes.mcp.gateway.skills.href(link.metaMcpServerId)
        : undefined;
      break;
    case "organization_gateway":
      href = orgRoutes.gateway.href();
      break;
  }

  return (
    <li className="flex items-center justify-between gap-3 px-4 py-3">
      <div className="min-w-0">
        {href ? (
          <Link
            to={href}
            className="block truncate text-sm font-medium hover:underline"
            title={name}
          >
            {name}
          </Link>
        ) : (
          <Text className="block truncate text-sm font-medium" title={name}>
            {name}
          </Text>
        )}
        <Text small muted className="block text-xs">
          {skillLinkTargetKindLabel(link.targetKind)}
        </Text>
      </div>
      <div className="flex shrink-0 items-center gap-2">
        <SkillDistributionVersionBadge pinnedVersionId={link.pinnedVersionId} />
        {canRemove && (
          <Button
            type="button"
            variant="tertiary"
            size="sm"
            tooltip="Stop serving"
            aria-label={`Remove from ${name}`}
            className="hover:text-destructive"
            disabled={isRemoving}
            onClick={onRemove}
          >
            <Trash2 className="h-4 w-4" />
          </Button>
        )}
      </div>
    </li>
  );
}

function linkTarget(link: SkillLink): SkillLinkTarget {
  switch (link.targetKind) {
    case "mcp_server":
      return { kind: "mcp_server", mcpServerId: link.mcpServerId ?? "" };
    case "meta_mcp_server":
      return {
        kind: "meta_mcp_server",
        metaMcpServerId: link.metaMcpServerId ?? "",
      };
    case "organization_gateway":
      return { kind: "organization_gateway" };
  }
}
