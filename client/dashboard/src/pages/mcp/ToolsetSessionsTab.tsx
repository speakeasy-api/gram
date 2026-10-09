import { ToolsetMcpTargetStatus } from "@/components/ToolsetMcpTargetStatus";
import { ToolsetAttachedUserSessions } from "@/components/sessions/AttachedUserSessions";
import { ClientsAndSessionsTab } from "@/components/sessions/ClientsAndSessionsTab";
import { useToolsetMcpTarget } from "@/hooks/useToolsetUrl";
import type { Toolset } from "@/lib/toolTypes";

export function ToolsetSessionsTab({
  toolset,
}: {
  toolset: Toolset;
}): JSX.Element {
  const target = useToolsetMcpTarget(toolset);
  if (target.status !== "ready") {
    return <ToolsetMcpTargetStatus status={target.status} />;
  }

  return (
    <ClientsAndSessionsTab
      issuerId={target.userSessionIssuerId}
      originatingMcpServerId={target.serverId}
      authTabPath="authentication"
      attachedSessions={<ToolsetAttachedUserSessions toolset={toolset} />}
    />
  );
}
