import { RequireScope } from "@/components/require-scope";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { Label } from "@/components/ui/Label";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useAgentsListPolicyGrants } from "@gram/client/react-query/agentsListPolicyGrants.js";
import { invalidateAllGetMcpServer } from "@gram/client/react-query/getMcpServer.js";
import {
  invalidateAllListMcpServersForOrg,
  useListMcpServersForOrg,
} from "@gram/client/react-query/listMcpServersForOrg.js";
import { useListProjects } from "@gram/client/react-query/listProjects.js";
import { invalidateAllMcpServers } from "@gram/client/react-query/mcpServers.js";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { Link } from "react-router";
import { toast } from "sonner";
import {
  attachableServers,
  permissionResourceId,
  policyGrantsConnect,
} from "./attachment";
import { failureMessage } from "./setupValues";

/**
 * Attaches one MCP server to the selected user session issuer, so the
 * platform's tokens reach it, and says what the chosen agent's policy holds
 * for it. One server at a time, by the same update the server's own settings
 * make; nothing is converted on the operator's behalf.
 *
 * The form is keyed by the issuer: switching issuers mounts a fresh one, so a
 * server chosen or a confirmation opened under the previous issuer cannot
 * come back when it is selected again.
 */
export function ServerAttachment({
  issuerId,
  issuerProjectId,
  agentId,
  agentName,
}: {
  /** The selected issuer, or "" while there is none. */
  issuerId: string;
  /** The selected issuer's project, or "" for an organization issuer. */
  issuerProjectId: string;
  /** The agent the access rule assigns, or "" when this flow chose none. */
  agentId: string;
  agentName: string | null;
}): JSX.Element | null {
  if (issuerId === "") return null;
  return (
    <AttachmentForIssuer
      key={issuerId}
      issuerId={issuerId}
      issuerProjectId={issuerProjectId}
      agentId={agentId}
      agentName={agentName}
    />
  );
}

function AttachmentForIssuer({
  issuerId,
  issuerProjectId,
  agentId,
  agentName,
}: {
  issuerId: string;
  issuerProjectId: string;
  agentId: string;
  agentName: string | null;
}): JSX.Element {
  const organization = useOrganization();
  const serversQuery = useListMcpServersForOrg(undefined, undefined, {
    throwOnError: false,
  });
  const projectsQuery = useListProjects(
    { organizationId: organization.id },
    undefined,
    { throwOnError: false },
  );
  const servers = useMemo(
    () =>
      attachableServers(serversQuery.data?.mcpServers ?? [], issuerProjectId),
    [serversQuery.data, issuerProjectId],
  );
  const projects = useMemo(
    () => projectsQuery.data?.projects ?? [],
    [projectsQuery.data],
  );
  const [chosenId, setChosenId] = useState("");
  const [confirming, setConfirming] = useState(false);
  const selected = servers.find((server) => server.id === chosenId);
  const selectedProject = projects.find((p) => p.id === selected?.projectId);
  const attach = useAttachServer(issuerId, selectedProject?.slug);

  const attached = servers.filter(
    (server) => server.userSessionIssuerId === issuerId,
  );
  const alreadyAttached = selected?.userSessionIssuerId === issuerId;
  const switching =
    selected !== undefined &&
    !alreadyAttached &&
    (selected.userSessionIssuerId ?? "") !== "";

  const onAttach = () => {
    if (selected === undefined) return;
    if (switching) {
      setConfirming(true);
      return;
    }
    attach.mutate(selected);
  };

  return (
    <Stack gap={3}>
      <Stack gap={1}>
        <p className="text-eyebrow">MCP servers on this authorization server</p>
        <Text muted small>
          {attached.length === 0
            ? "None yet. Attach the MCP server Claude Tag should reach; its tokens are only accepted by servers attached here."
            : attached
                .map((server) => serverLabel(server, projects))
                .join(", ")}
        </Text>
      </Stack>
      <Stack gap={2}>
        <Label htmlFor="setup-attach-server">Attach an MCP server</Label>
        <Select value={chosenId} onValueChange={setChosenId}>
          <SelectTrigger id="setup-attach-server" className="w-full">
            <SelectValue
              placeholder={serversPlaceholder(
                serversQuery.isPending,
                serversQuery.isError,
                servers.length,
              )}
            />
          </SelectTrigger>
          <SelectContent>
            {groupServers(servers, projects).map((group) => (
              <SelectGroup key={group.projectId}>
                <SelectLabel>{group.label}</SelectLabel>
                {group.servers.map((server) => (
                  <SelectItem key={server.id} value={server.id}>
                    {server.name ?? server.slug ?? server.id}
                  </SelectItem>
                ))}
              </SelectGroup>
            ))}
          </SelectContent>
        </Select>
        <Text muted small>
          Disabled and private-only servers are left out. Choose a server
          reached on a Speakeasy host: one reached only on a custom domain is
          still listed here but cannot be connected this way.
        </Text>
        {selected !== undefined && !alreadyAttached && (
          <div>
            <RequireScope
              scope="mcp:write"
              resourceId={permissionResourceId(selected)}
              projectId={selected.projectId}
              level="component"
            >
              {({ disabled }) => (
                <Button
                  size="sm"
                  disabled={disabled || attach.isPending}
                  onClick={onAttach}
                >
                  <Button.Text>
                    {attach.isPending ? "Attaching…" : "Attach server"}
                  </Button.Text>
                </Button>
              )}
            </RequireScope>
          </div>
        )}
        {selected !== undefined && alreadyAttached && (
          <Text muted small>
            Already attached to this authorization server.
          </Text>
        )}
        {attach.isError && (
          <Text role="alert" small destructive>
            {failureMessage(attach.error, "Failed to attach the server")}
          </Text>
        )}
      </Stack>
      {selected !== undefined && agentId !== "" && (
        <AgentAccess
          server={selected}
          projectSlug={selectedProject?.slug}
          agentId={agentId}
          agentName={agentName}
        />
      )}
      {selected !== undefined && (
        <ConfirmDialog
          open={confirming}
          onOpenChange={setConfirming}
          title="Change this server's authorization server?"
          description={`${serverLabel(selected, projects)} already uses another authorization server. Everyone signed in to it must sign in again, and tokens its current authorization server issued stop working for it.`}
          confirmLabel="Change and attach"
          pendingLabel="Attaching…"
          isPending={attach.isPending}
          onConfirm={() => {
            attach.mutate(selected, { onSettled: () => setConfirming(false) });
          }}
        />
      )}
    </Stack>
  );
}

/**
 * Attaches a server by the same full-record update its settings page makes.
 * The latest snapshot is fetched first so the fields this does not change are
 * sent with their current values rather than the list's; a change landing
 * between that read and the update is not guarded against here. The project
 * header names the server's own project, since this runs from an
 * organization-level page.
 */
function useAttachServer(issuerId: string, projectSlug: string | undefined) {
  const client = useSdkClient();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (server: McpServer) => {
      if (projectSlug === undefined) {
        throw new Error("The server's project could not be resolved");
      }
      const latest = await client.mcpServers.get({
        id: server.id,
        gramProject: projectSlug,
      });
      await client.mcpServers.update({
        gramProject: projectSlug,
        updateMcpServerForm: {
          id: latest.id,
          environmentId: latest.environmentId,
          networkAccessMode: latest.networkAccessMode,
          remoteMcpServerId: latest.remoteMcpServerId,
          tunneledMcpServerId: latest.tunneledMcpServerId,
          toolsetId: latest.toolsetId,
          unproxiedMcpServerId: latest.unproxiedMcpServerId,
          toolVariationsGroupId: latest.toolVariationsGroupId,
          userSessionIssuerId: issuerId,
          visibility: latest.visibility,
        },
      });
      return latest;
    },
    onSuccess: async (latest) => {
      toast.success(`${latest.name ?? "Server"} attached`);
      await Promise.all([
        invalidateAllListMcpServersForOrg(queryClient, { refetchType: "all" }),
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
        invalidateAllGetMcpServer(queryClient, { refetchType: "all" }),
      ]);
    },
  });
}

/**
 * What the chosen agent's configured policy says about this server, with the
 * way to its policy page. Only the grants are read: whether a connection is
 * admitted is decided by the server from the whole policy, so this points at
 * what to check rather than promising an outcome, and never writes a grant.
 */
function AgentAccess({
  server,
  projectSlug,
  agentId,
  agentName,
}: {
  server: McpServer;
  projectSlug: string | undefined;
  agentId: string;
  agentName: string | null;
}): JSX.Element | null {
  const grants = useAgentsListPolicyGrants({ agentId }, undefined, {
    throwOnError: false,
  });
  const routes = useRoutes({ projectSlug });
  const name = agentName ?? "The agent";
  const serverName = server.name ?? "this server";
  if (grants.isPending) return null;
  const policyLink = (
    <Link
      className="underline underline-offset-4"
      to={`${routes.agents.href()}?id=${encodeURIComponent(agentId)}`}
    >
      {name}&apos;s policy
    </Link>
  );
  if (grants.isError) {
    return (
      <Text muted small>
        {name}&apos;s policy could not be read. Check {policyLink} before
        connecting: a session is only admitted to a server the policy allows.
      </Text>
    );
  }
  if (policyGrantsConnect(grants.data ?? [], server)) {
    return (
      <Text muted small>
        {policyLink} has a grant that allows connecting to {serverName}. The
        server checks the whole policy on every connection, so review it there
        if a session is still refused.
      </Text>
    );
  }
  return (
    <Alert variant="warning" alignTop>
      <div className="text-sm">
        No direct mcp:connect grant for {serverName} is shown in {policyLink}.
        Review it before connecting; the server decides effective access.
      </div>
    </Alert>
  );
}

/** A project as the attachment needs it: its id to match, its slug and name to show. */
type ProjectRef = { id: string; slug: string; name: string };

function serverLabel(server: McpServer, projects: ProjectRef[]): string {
  const project = projects.find((p) => p.id === server.projectId);
  const name = server.name ?? server.slug ?? server.id;
  return project === undefined ? name : `${name} (${project.name})`;
}

function groupServers(
  servers: McpServer[],
  projects: ProjectRef[],
): { projectId: string; label: string; servers: McpServer[] }[] {
  const groups = new Map<string, McpServer[]>();
  for (const server of servers) {
    groups.set(server.projectId, [
      ...(groups.get(server.projectId) ?? []),
      server,
    ]);
  }
  return [...groups.entries()].map(([projectId, group]) => ({
    projectId,
    label: `Project: ${projects.find((p) => p.id === projectId)?.name ?? projectId}`,
    servers: group,
  }));
}

function serversPlaceholder(
  isPending: boolean,
  isError: boolean,
  count: number,
): string {
  if (isPending) return "Loading MCP servers…";
  if (isError) return "MCP servers could not be loaded";
  if (count === 0) return "No MCP server can be attached yet";
  return "Select an MCP server";
}
