import { Link } from "react-router";

import { SettingsSection } from "@/components/detail/settings-section";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { StatusDot, type StatusDotTone } from "@/components/ui/StatusDot";
import { type Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { formatRelativeTime } from "@/lib/dates";
import { pluralize } from "@/lib/format";
import { mcpServerRouteParam } from "@/lib/sources";
import { useRoutes } from "@/routes";
import {
  EMAServiceServerStatus,
  type EMAServiceServer,
} from "@gram/client/models/components/emaserviceserver.js";
import { isKnownEnumValue } from "@/lib/enum";

import { IDENTITY_CHAINING_SECTION_ID } from "@/pages/mcp/x/tabs/settings/sections/authentication/IdentityChainingSection";
import {
  serverDisplayName,
  strandedReason,
  strandedServers,
  supportsProvider,
} from "./emaServiceView";
import { type ServiceRow } from "./useEmaServices";

/** A server's state, in the words its own settings use. */
function serverStatus(
  server: EMAServiceServer,
  stranded: boolean,
  service: string,
): {
  label: string;
  reason?: string;
  tone: StatusDotTone;
} {
  if (!isKnownEnumValue(EMAServiceServerStatus, server.status))
    return { label: "Unknown", tone: "neutral" };
  if (stranded)
    return {
      label: "Needs attention",
      reason: strandedReason(server, service),
      tone: "destructive",
    };
  switch (server.status) {
    case "working":
      return { label: "Working", tone: "success" };
    case "not_proven":
      return { label: "On, waiting for a first connection", tone: "warning" };
    case "needs_attention":
      return { label: "Needs attention", tone: "destructive" };
  }
  return { label: "Unknown", tone: "neutral" };
}

/** The server's Enterprise Managed Authorization settings, in its own project. */
function ServerLink({ server }: { server: EMAServiceServer }): JSX.Element {
  const routes = useRoutes({ projectSlug: server.projectSlug });
  const name = serverDisplayName(server);
  return (
    <Link
      to={`${routes.mcp.details.settings.href(mcpServerRouteParam(server))}#${IDENTITY_CHAINING_SECTION_ID}`}
      className="block truncate font-medium underline-offset-4 hover:underline"
      title={name}
    >
      {name}
    </Link>
  );
}

/** Which servers connect to a service, each linking to where it is turned on or off. */
export function EmaServiceServers({ row }: { row: ServiceRow }): JSX.Element {
  const stranded = new Set(
    strandedServers(
      row.isDefault || !row.providers.every(supportsProvider)
        ? null
        : row.providers,
      row.servers,
    ).map((server) => server.id),
  );
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
      width: "260px",
      render: (server) => {
        const status = serverStatus(server, stranded.has(server.id), row.name);
        return (
          <span className="flex min-w-0 flex-col">
            <span className="flex min-w-0 items-center gap-2">
              <StatusDot tone={status.tone} />
              <Text as="span" small className="truncate">
                {status.label}
              </Text>
            </span>
            {status.reason && (
              <Text
                as="span"
                muted
                small
                className="truncate pl-4"
                title={status.reason}
              >
                {status.reason}
              </Text>
            )}
            {server.lastAcceptedAt && (
              <Text as="span" muted small className="truncate pl-4">
                Last worked {formatRelativeTime(server.lastAcceptedAt)}
              </Text>
            )}
          </span>
        );
      },
    },
  ];
  const hidden = row.hiddenServerCount;
  return (
    <SettingsSection id="servers">
      <SettingsSection.Header>
        <SettingsSection.Title>Affected servers</SettingsSection.Title>
        <SettingsSection.Description>
          MCP servers that connect to {row.name} through Enterprise Managed
          Authorization. The settings on this page apply to all of them. Turn
          Enterprise Managed Authorization on or off in each server’s settings.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        {row.servers.length === 0 ? (
          <SettingsSection.Body>
            <InlineEmptyState
              icon="server"
              heading={
                hidden > 0
                  ? `No servers you can see connect to ${row.name}`
                  : `No servers connect to ${row.name} yet`
              }
              description={
                hidden > 0
                  ? `${pluralize(hidden, "server")} you don’t have access to ${hidden === 1 ? "connects" : "connect"} to it.`
                  : "Turn on Enterprise Managed Authorization in an MCP server’s settings. The server then shows up here."
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
