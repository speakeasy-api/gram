import { Link } from "react-router";

import { SettingsSection } from "@/components/detail/settings-section";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { StatusDot, type StatusDotTone } from "@/components/ui/StatusDot";
import { type Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { formatRelativeTime } from "@/lib/dates";
import { mcpServerRouteParam } from "@/lib/sources";
import { useRoutes } from "@/routes";
import type { EMAServiceServer } from "@gram/client/models/components/emaserviceserver.js";

import { IDENTITY_CHAINING_SECTION_ID } from "@/pages/mcp/x/tabs/settings/sections/authentication/IdentityChainingSection";
import { serverCountLabel, type ServiceRow } from "./useEmaServices";

/** A server's state, in the words its own settings use. */
function serverStatus(server: EMAServiceServer): {
  label: string;
  tone: StatusDotTone;
} {
  switch (server.status) {
    case "working":
      return { label: "On and working", tone: "success" };
    case "not_proven":
      return { label: "On, not proven yet", tone: "warning" };
    case "needs_attention":
      return { label: "Needs attention", tone: "destructive" };
  }
}

/** The server's Enterprise Managed Authorization settings, in its own project. */
function ServerLink({ server }: { server: EMAServiceServer }): JSX.Element {
  const routes = useRoutes({ projectSlug: server.projectSlug });
  const name = server.name.trim() || server.slug || "Unnamed server";
  return (
    <Link
      to={`${routes.mcp.x.settings.href(mcpServerRouteParam(server))}#${IDENTITY_CHAINING_SECTION_ID}`}
      className="block truncate font-medium underline-offset-4 hover:underline"
      title={name}
    >
      {name}
    </Link>
  );
}

/** Which servers reach a service, each linking to where it is turned on or off. */
export function EmaServiceServers({ row }: { row: ServiceRow }): JSX.Element {
  const columns: Column<EMAServiceServer>[] = [
    {
      key: "server",
      header: "Server",
      render: (server) => <ServerLink server={server} />,
    },
    {
      key: "project",
      header: "Project",
      width: "200px",
      render: (server) => (
        <Text small muted className="truncate" title={server.projectName}>
          {server.projectName}
        </Text>
      ),
    },
    {
      key: "status",
      header: "Status",
      width: "220px",
      render: (server) => {
        const status = serverStatus(server);
        return (
          <span
            className="flex min-w-0 items-center gap-2"
            title={
              server.lastAcceptedAt
                ? `${row.name} last accepted a sign-in here ${formatRelativeTime(server.lastAcceptedAt)}`
                : undefined
            }
          >
            <StatusDot tone={status.tone} />
            <Text as="span" small className="truncate">
              {status.label}
            </Text>
          </span>
        );
      },
    },
  ];
  const hidden = row.hiddenServerCount;
  return (
    <SettingsSection id="servers">
      <SettingsSection.Header>
        <SettingsSection.Title>Servers</SettingsSection.Title>
        <SettingsSection.Description>
          MCP servers that reach {row.name} through Enterprise Managed
          Authorization. Turn it on or off in each server’s settings.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        {row.servers.length === 0 ? (
          <SettingsSection.Body>
            <InlineEmptyState
              icon="server"
              heading={
                hidden > 0
                  ? `No servers you can see reach ${row.name}`
                  : `No servers reach ${row.name} yet`
              }
              description={
                hidden > 0
                  ? `${serverCountLabel(hidden)} you don’t have access to ${hidden === 1 ? "reaches" : "reach"} it.`
                  : "Turn on Enterprise Managed Authorization in an MCP server’s settings and it shows up here."
              }
            />
          </SettingsSection.Body>
        ) : (
          <>
            <Table
              columns={columns}
              data={row.servers}
              rowKey={(server) => server.id}
            />
            {hidden > 0 && (
              <SettingsSection.Body>
                <Text muted small>
                  {hidden === 1
                    ? "1 more server you don’t have access to."
                    : `${hidden} more servers you don’t have access to.`}
                </Text>
              </SettingsSection.Body>
            )}
          </>
        )}
      </SettingsSection.Panel>
    </SettingsSection>
  );
}
