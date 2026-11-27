import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { ChevronRight } from "lucide-react";
import { Checkbox } from "@/components/ui/Checkbox";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useSession } from "@/contexts/Auth";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { invalidateAgentPolicy } from "@/pages/agents/agent-policy-grants";
import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useAgent } from "@gram/client/react-query/agent.js";
import { useAgentsCreatePolicyGrantMutation } from "@gram/client/react-query/agentsCreatePolicyGrant.js";
import { useAgentsDeletePolicyGrantMutation } from "@gram/client/react-query/agentsDeletePolicyGrant.js";
import {
  invalidateAgentsListPolicyGrants,
  useAgentsListPolicyGrants,
} from "@gram/client/react-query/agentsListPolicyGrants.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import {
  invalidateResourceAudience,
  useResourceAudience,
} from "@gram/client/react-query/resourceAudience.js";
import { useQueryClient } from "@tanstack/react-query";
import { useState, type JSX } from "react";
import { toast } from "sonner";
import { agentConnectAccess, directServerConnectGrants } from "./kitMCPAccess";
import { Section } from "./PanelSection";

export function AssistantKitMCPAccess({
  agentId,
}: {
  agentId?: string;
}): JSX.Element {
  return (
    <Section title="MCP servers">
      {agentId ? (
        <ProjectMCPAccess key={agentId} agentId={agentId} />
      ) : (
        <Text small muted>
          Set up this assistant's agent identity to manage its MCP access.
        </Text>
      )}
    </Section>
  );
}

function ProjectMCPAccess({ agentId }: { agentId: string }) {
  const gramProject = useProjectSlugForRequests();
  const servers = useMcpServers({ gramProject }, undefined, {
    throwOnError: false,
  });
  const agent = useAgent({ id: agentId }, undefined, { throwOnError: false });
  const canWrite = agent.data?.permissions.write === true;
  const grants = useAgentsListPolicyGrants({ agentId }, undefined, {
    enabled: canWrite,
    throwOnError: false,
  });

  if (servers.isError)
    return (
      <Text small muted>
        Couldn't load project MCP servers.
      </Text>
    );
  if (!servers.data)
    return <Skeleton label="Loading project MCP servers" className="h-8" />;
  const available = [...servers.data.mcpServers].sort((a, b) =>
    serverName(a).localeCompare(serverName(b)),
  );
  if (available.length === 0)
    return (
      <Text small muted>
        No MCP servers in this project.
      </Text>
    );

  return (
    <div className="space-y-2">
      <Text small muted>
        Check a server to give the agent direct Connect access. Access from
        other rules can remain after you uncheck it.
      </Text>
      {agent.isError || grants.isError ? (
        <Text small muted>
          Couldn't load the agent's direct grants.
        </Text>
      ) : agent.data && !canWrite ? (
        <Text small muted>
          Only people who can change this agent can edit its direct grants.
        </Text>
      ) : null}
      <ul className="divide-border divide-y">
        {available.map((server) => (
          <ProjectMCPAccessRow
            key={server.id}
            agentId={agentId}
            server={server}
            grants={canWrite && !grants.isError ? grants.data : undefined}
          />
        ))}
      </ul>
    </div>
  );
}

function serverName(server: McpServer): string {
  return server.name ?? server.slug ?? server.id;
}

function ProjectMCPAccessRow({
  agentId,
  server,
  grants,
}: {
  agentId: string;
  server: McpServer;
  grants: AgentPolicyGrant[] | undefined;
}) {
  const session = useSession();
  const queryClient = useQueryClient();
  const resourceId = server.toolsetId ?? server.id;
  const resource = { resourceKind: "mcp", resourceId } as const;
  const add = useAgentsCreatePolicyGrantMutation();
  const remove = useAgentsDeletePolicyGrantMutation();
  const [saving, setSaving] = useState(false);
  const direct = directServerConnectGrants(
    grants ?? [],
    server.projectId,
    resourceId,
  );
  const checked = direct.length > 0;

  const save = async (next: boolean) => {
    if (!grants || saving || checked === next) return;
    setSaving(true);
    try {
      if (next) {
        await add.mutateAsync({
          request: {
            createAgentPolicyGrantForm: {
              agentId,
              effect: "allow",
              scope: "mcp:connect",
              selector: {
                resourceKind: "mcp",
                resourceId,
                projectId: server.projectId,
              },
            },
          },
        });
      } else {
        for (const { id: grantId, scope, selector } of direct) {
          await remove.mutateAsync({
            request: {
              deleteAgentPolicyGrantForm: {
                agentId,
                grantId,
                expectedPolicy: { scope, selector },
              },
            },
          });
        }
      }
    } catch {
      toast.error("Couldn't update the agent's Connect access.");
    } finally {
      await Promise.all([
        invalidateAgentsListPolicyGrants(queryClient, [{ agentId }]),
        invalidateResourceAudience(queryClient, [resource]),
        invalidateAgentPolicy(
          queryClient,
          session.activeOrganizationId,
          session.user.id,
          agentId,
        ),
      ]);
      setSaving(false);
    }
  };

  return (
    <li className="flex items-start gap-2 py-2">
      {grants !== undefined && (
        <Checkbox
          aria-label={`Direct Connect access to ${serverName(server)}`}
          checked={checked}
          disabled={saving || (server.visibility === "disabled" && !checked)}
          onCheckedChange={(value) => void save(value === true)}
        />
      )}
      <div className="min-w-0">
        <Text small>{serverName(server)}</Text>
        {server.visibility === "disabled" ? (
          <Text small muted>
            Server disabled
          </Text>
        ) : (
          <Collapsible>
            <CollapsibleTrigger
              aria-label={`Check agent access to ${serverName(server)}`}
              className="text-muted-foreground hover:text-foreground flex items-center gap-1 text-xs [&[data-state=open]>svg]:rotate-90"
            >
              <ChevronRight className="size-3" aria-hidden />
              Check agent access
            </CollapsibleTrigger>
            <CollapsibleContent>
              <AgentConnectAccess agentId={agentId} resourceId={resourceId} />
            </CollapsibleContent>
          </Collapsible>
        )}
      </div>
    </li>
  );
}

function AgentConnectAccess({
  agentId,
  resourceId,
}: {
  agentId: string;
  resourceId: string;
}): JSX.Element {
  const audience = useResourceAudience(
    { resourceKind: "mcp", resourceId },
    undefined,
    {
      retry: false,
      throwOnError: false,
    },
  );
  const access = audience.isError
    ? "Access unavailable"
    : audience.data
      ? agentConnectAccess(audience.data.entries, agentId)
      : "Checking access…";
  return (
    <Text small muted>
      Agent access: {access}
    </Text>
  );
}
