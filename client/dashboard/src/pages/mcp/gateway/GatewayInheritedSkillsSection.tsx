import { InlineEmptyState } from "@/components/inline-empty-state";
import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/Button";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { type Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { useDrainInfiniteQuery } from "@/hooks/useDrainInfiniteQuery";
import { usePluginQueryScope } from "@/pages/plugins/usePluginQueryScope";
import { useRoutes } from "@/routes";
import { useMetaMcpMembers } from "@gram/client/react-query/metaMcpMembers.js";
import { useSkillLinksInfinite } from "@gram/client/react-query/skillLinks.js";
import { Fragment, useMemo } from "react";
import { Link } from "react-router";
import {
  type InheritedSkill,
  type InheritedSkillServer,
  inheritedSkills,
} from "./gatewayInheritedSkills";

/**
 * The skills a gateway serves because they are linked to its member servers.
 * Read-only here: a skill is added or removed on the member server's own
 * Skills tab. Agents are served a member's skills only while they can reach
 * that member through the gateway.
 */
export function GatewayInheritedSkillsSection({
  metaMcpServerId,
  surfaceName,
}: {
  metaMcpServerId: string;
  surfaceName: string;
}): JSX.Element {
  const scope = usePluginQueryScope();
  const membersQuery = useMetaMcpMembers({ metaMcpServerId }, undefined, {
    throwOnError: false,
  });
  const linksQuery = useSkillLinksInfinite(
    { ...scope, targetKind: "mcp_server", limit: 50 },
    undefined,
    { throwOnError: false },
  );
  useDrainInfiniteQuery(linksQuery);

  const skills = useMemo(
    () =>
      inheritedSkills(
        linksQuery.data?.pages.flatMap((page) => page.result.links) ?? [],
        membersQuery.data?.members ?? [],
      ),
    [linksQuery.data?.pages, membersQuery.data?.members],
  );
  const isLoaded =
    !!membersQuery.data && !!linksQuery.data && !linksQuery.hasNextPage;

  let body: JSX.Element;
  if (membersQuery.error || linksQuery.error) {
    body = (
      <InlineEmptyState
        icon="triangle-alert"
        heading="Couldn't load inherited skills"
        description={`The skills ${surfaceName} inherits from its servers could not be loaded.`}
        action={
          <Button
            size="sm"
            variant="secondary"
            onClick={() => {
              if (membersQuery.error) void membersQuery.refetch();
              if (linksQuery.error)
                void (linksQuery.data && linksQuery.hasNextPage
                  ? linksQuery.fetchNextPage()
                  : linksQuery.refetch());
            }}
          >
            Retry
          </Button>
        }
      />
    );
  } else if (!isLoaded) {
    body = <SkeletonTable />;
  } else if (skills.length === 0) {
    body = (
      <InlineEmptyState
        icon="sparkles"
        heading="No inherited skills"
        description={`Skills added to a server in ${surfaceName} show up here.`}
      />
    );
  } else {
    body = (
      <Table
        columns={inheritedSkillColumns}
        data={skills}
        rowKey={(skill) => skill.skillId}
      />
    );
  }

  return (
    <Page.Section>
      <Page.Section.Title>Inherited from servers</Page.Section.Title>
      <Page.Section.Description>
        Skills added to servers in {surfaceName}. Agents load them here while
        they can reach that server. Manage them on the server&apos;s Skills tab.
      </Page.Section.Description>
      <Page.Section.Body>{body}</Page.Section.Body>
    </Page.Section>
  );
}

const inheritedSkillColumns: Column<InheritedSkill>[] = [
  {
    key: "skill",
    header: "Skill",
    width: "1fr",
    render: (skill) => <InheritedSkillCell skill={skill} />,
  },
  {
    key: "servers",
    header: "From",
    width: "1fr",
    render: (skill) => <InheritedFromCell servers={skill.servers} />,
  },
];

function InheritedSkillCell({ skill }: { skill: InheritedSkill }): JSX.Element {
  const routes = useRoutes();
  return (
    <div className="min-w-0">
      <Link
        to={routes.skills.detail.href(skill.skillId)}
        className="block truncate text-sm font-medium hover:underline"
        title={skill.skillDisplayName}
      >
        {skill.skillDisplayName}
      </Link>
      <Text small muted className="block truncate font-mono text-xs">
        {skill.skillName}
      </Text>
    </div>
  );
}

function InheritedFromCell({
  servers,
}: {
  servers: InheritedSkillServer[];
}): JSX.Element {
  const routes = useRoutes();
  return (
    <Text
      small
      muted
      className="block truncate"
      title={servers.map((server) => server.name).join(", ")}
    >
      {servers.map((server, index) => (
        <Fragment key={server.routeParam}>
          {index > 0 && ", "}
          <Link
            to={routes.mcp.details.skills.href(server.routeParam)}
            className="hover:underline"
          >
            {server.name}
          </Link>
        </Fragment>
      ))}
    </Text>
  );
}
