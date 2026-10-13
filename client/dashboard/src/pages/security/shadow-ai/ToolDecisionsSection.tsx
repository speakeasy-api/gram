import { CollapsibleSectionHeader } from "@/components/collapsible-section-header";
import { ShadowAISetupEmptyState } from "@/components/setup-empty-state";
import { ShadowMCPInventoryTable } from "@/components/shadow-mcp/ShadowMCPInventoryTable";
import { eligibleShadowMCPAllowRulePolicies } from "@/components/shadow-mcp/shadowMCPInventoryStatus";
import { AIToolDecisionTable } from "@/components/shadow-ai/AIToolDecisionTable";
import { Alert, AlertDescription } from "@/components/ui/Alert";
import { Card } from "@/components/ui/Card";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import { useRoutes } from "@/routes";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { AIDetection } from "@gram/client/models/components/aidetection.js";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import type { Role } from "@gram/client/models/components/role.js";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { useAiDetections } from "@gram/client/react-query/aiDetections.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { useRiskListPolicies } from "@gram/client/react-query/riskListPolicies.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import { useMemo, useState } from "react";
import { SectionHeader } from "../policy-editor-shell";

type AIToolCategory = AIDetection["category"];

type AIToolGroup = {
  category: AIToolCategory;
  label: string;
  description: string;
  noun: string;
  defaultExpanded: boolean;
};

const AI_TOOL_GROUPS: readonly AIToolGroup[] = [
  {
    category: "harness",
    label: "Harnesses",
    description:
      "Agentic coding tools and AI IDEs found on devices, such as Cursor and Claude Code.",
    noun: "harnesses",
    defaultExpanded: true,
  },
  {
    category: "assistant",
    label: "Assistants",
    description: "Desktop assistants found on devices that speak MCP.",
    noun: "assistants",
    defaultExpanded: true,
  },
  {
    category: "local_model",
    label: "Local models",
    description:
      "Model runtimes found on devices. They never reach the gateway, so they are listed, not decided.",
    noun: "local models",
    defaultExpanded: false,
  },
];

const MCP_GROUP_KEY = "mcp";

type ExpandedGroups = Record<string, boolean>;

function initialExpandedGroups(): ExpandedGroups {
  const expanded: ExpandedGroups = { [MCP_GROUP_KEY]: true };
  for (const group of AI_TOOL_GROUPS) {
    expanded[group.category] = group.defaultExpanded;
  }
  return expanded;
}

function groupDetections(
  detections: readonly AIDetection[],
): Map<AIToolCategory, AIDetection[]> {
  const byCategory = new Map<AIToolCategory, AIDetection[]>();
  for (const group of AI_TOOL_GROUPS) byCategory.set(group.category, []);
  for (const detection of detections) {
    byCategory.get(detection.category)?.push(detection);
  }
  return byCategory;
}

/** Nothing to list: either nothing was detected, or the read failed, which
 *  must not read as "nothing detected". */
function AIToolGroupEmptyState({
  failed,
  noun,
}: {
  failed: boolean;
  noun: string;
}): JSX.Element {
  if (failed) {
    return (
      <Alert variant="error" className="m-4 w-auto">
        <AlertDescription>
          Detected {noun} could not be loaded. Reload to try again.
        </AlertDescription>
      </Alert>
    );
  }
  return <ShadowAISetupEmptyState heading={`No ${noun} detected yet`} />;
}

/** The MCP servers group: the inventory table once the policy set, members
 *  and roles are known, a skeleton meanwhile, and an error when the policy
 *  list failed, since the decide sheet picks its write path from that set and
 *  a failed read must not pass for "no policy". */
function MCPServersGroupBody({
  policiesFailed,
  ready,
  members,
  roles,
  policies,
  projectId,
  onOpenServer,
}: {
  policiesFailed: boolean;
  ready: boolean;
  members: AccessMember[];
  roles: Role[];
  policies: RiskPolicy[] | undefined;
  projectId: string;
  onOpenServer: (server: ShadowMCPInventoryServer) => void;
}): JSX.Element {
  if (policiesFailed) {
    return (
      <Alert variant="error">
        <AlertDescription>
          The project's policies could not be loaded, so MCP server decisions
          are unavailable. Reload to try again.
        </AlertDescription>
      </Alert>
    );
  }
  if (!ready) return <SkeletonTable />;
  return (
    <ShadowMCPInventoryTable
      members={members}
      onOpenServer={onOpenServer}
      projectID={projectId}
      roles={roles}
      shadowMCPPolicies={eligibleShadowMCPAllowRulePolicies(policies)}
    />
  );
}

/** What "unreviewed" means under this policy, said once above the groups. The
 *  server reads a missing disposition as block_all, as the standard editor
 *  does. */
function PostureNote({ policy }: { policy: RiskPolicy }): JSX.Element {
  if (policy.shadowMcpDisposition === "allow_all") {
    return (
      <Text small muted>
        Unreviewed tools and servers stay allowed until you disallow them.
      </Text>
    );
  }
  return (
    <Alert variant="info" alignTop>
      <AlertDescription>
        Unreviewed MCP servers are blocked by this policy's default, so allow
        the ones your teams need. Unreviewed harnesses and assistants still
        connect until you disallow them.
      </AlertDescription>
    </Alert>
  );
}

/**
 * The inventory a Shadow AI policy decides over: every harness, assistant
 * and local model the device agent found, and every MCP server seen in agent
 * traffic, each group collapsible with its decision inline. AI tool decisions
 * are the organization's and apply at once; MCP server decisions attach to
 * this policy's review of record.
 */
export function ToolDecisionsSection({
  policy,
}: {
  policy: RiskPolicy;
}): JSX.Element {
  const routes = useRoutes();
  const project = useProject();
  const detectionsQuery = useAiDetections();
  const policiesQuery = useRiskListPolicies();
  const membersQuery = useMembers();
  const rolesQuery = useRoles();
  const [expanded, setExpanded] = useState<ExpandedGroups>(
    initialExpandedGroups,
  );
  const byCategory = useMemo(
    () => groupDetections(detectionsQuery.data?.detections ?? []),
    [detectionsQuery.data?.detections],
  );
  const toolRoutes = {
    harness: routes.shadowAI.harnesses.detail,
    assistant: routes.shadowAI.assistants.detail,
    local_model: routes.shadowAI.models.detail,
  };
  // The decide sheet words its form and picks its write path from the
  // policy set's disposition, so no MCP decision is offered until that set
  // is known: a failed read must not pass for "no policy".
  const mcpReady =
    !!policiesQuery.data &&
    (membersQuery.isError || !!membersQuery.data) &&
    (rolesQuery.isError || !!rolesQuery.data);
  const toggle = (key: string) =>
    setExpanded((current) => ({ ...current, [key]: !current[key] }));

  return (
    <Card>
      <SectionHeader
        title="Tool decisions"
        description="Approve or disallow each tool. A disallowed tool is refused when it connects to the MCP gateway; a disallowed MCP server is refused on every tool call. Harness and assistant decisions apply across the organization; this policy's audience scopes MCP server decisions only."
      />
      <PostureNote policy={policy} />
      <div className="divide-border border divide-y">
        {AI_TOOL_GROUPS.map((group) => {
          const detections = byCategory.get(group.category) ?? [];
          return (
            <div key={group.category}>
              <CollapsibleSectionHeader
                label={group.label}
                description={group.description}
                count={detectionsQuery.data ? detections.length : undefined}
                expanded={!!expanded[group.category]}
                onToggle={() => toggle(group.category)}
              />
              {expanded[group.category] ? (
                <div className="border-border border-t">
                  <AIToolDecisionTable
                    detections={detections}
                    isLoading={detectionsQuery.isPending}
                    emptyState={
                      <AIToolGroupEmptyState
                        failed={detectionsQuery.isError}
                        noun={group.noun}
                      />
                    }
                    onOpenTool={(detection) =>
                      toolRoutes[group.category].goTo(
                        encodeURIComponent(detection.targetId),
                      )
                    }
                  />
                </div>
              ) : null}
            </div>
          );
        })}
        <div>
          <CollapsibleSectionHeader
            label="MCP servers"
            description="Servers observed in agent traffic that no Speakeasy toolset manages. Decisions here create the review of record."
            expanded={!!expanded[MCP_GROUP_KEY]}
            onToggle={() => toggle(MCP_GROUP_KEY)}
          />
          {expanded[MCP_GROUP_KEY] ? (
            <div className="border-border border-t px-4 py-4">
              <MCPServersGroupBody
                policiesFailed={policiesQuery.isError}
                ready={mcpReady}
                members={membersQuery.data?.members ?? []}
                roles={rolesQuery.data?.roles ?? []}
                policies={policiesQuery.data?.policies}
                projectId={project.id}
                onOpenServer={(server) =>
                  routes.shadowAI.mcps.detail.goTo(server.serverSlug)
                }
              />
            </div>
          ) : null}
        </div>
      </div>
    </Card>
  );
}
