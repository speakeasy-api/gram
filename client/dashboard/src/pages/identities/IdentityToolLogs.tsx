import { useMemo } from "react";
import { Text } from "@/components/ui/Text";
import { LogsTools } from "@/components/observe/LogsTools";
import { useToolLogsAccess } from "@/components/observe/toolLogsAccess";
import { useSession } from "@/contexts/Auth";
import type { IdentityModel } from "@gram/client/models/components/identitymodel.js";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import type { ToolUsageUserFilter } from "@gram/client/models/components/toolusageuserfilter.js";

/**
 * Every identifier telemetry may have recorded this identity's calls under.
 * An agent is keyed by its id alone: its owner's own calls are not its calls.
 */
function identityToolLogFilters(
  identity: IdentityModel | undefined,
  agentId?: string,
): ToolUsageUserFilter[] {
  if (agentId) return [{ kind: "agent_id", key: agentId }];
  if (!identity) return [];
  return [
    ...identity.userIds.map((key) => ({ kind: "user_id" as const, key })),
    ...identity.externalUserIds.map((key) => ({
      kind: "external_user_id" as const,
      key,
    })),
    ...identity.emails.map((key) => ({ kind: "email" as const, key })),
  ];
}

/**
 * The Tool Logs view from Observability, narrowed to one identity: the rows
 * behind the usage numbers above it.
 */
export function IdentityToolLogsPanel({
  identity,
  agent,
  accountType,
  includeLocalTools,
}: {
  /** A person; an agent passes agent instead. */
  identity?: IdentityModel;
  agent?: Pick<
    ManagedAgent,
    "id" | "ownerUserId" | "ownerReassignmentRequiredAt"
  >;
  /** The page's account filter: "team", "personal", or "" for every account. */
  accountType?: string;
  /** The page's "Show local tools" choice; omitted shows every type. */
  includeLocalTools?: boolean;
}): JSX.Element {
  const { user } = useSession();
  const { projectWide } = useToolLogsAccess();
  // Without log:read the server returns only the viewer's own activity, so
  // this view is honest only for the viewer themself or an agent they own;
  // anyone else's panel would say "no calls" about calls that exist.
  const ownIdentity =
    (identity?.userIds.includes(user.id) ?? false) ||
    (agent !== undefined &&
      agent.ownerUserId === user.id &&
      !agent.ownerReassignmentRequiredAt);
  const canViewToolLogs = projectWide || ownIdentity;
  const userFilters = useMemo(
    () => identityToolLogFilters(identity, agent?.id),
    [identity, agent?.id],
  );

  let body: JSX.Element;
  if (!canViewToolLogs) {
    body = (
      <Text muted>
        Tool logs of other members and their agents need the View tool logs
        permission on this project.
      </Text>
    );
  } else if (userFilters.length === 0) {
    // An empty scope would show every identity's calls, not none.
    body = <Text muted>No tool calls can be attributed to this identity.</Text>;
  } else {
    body = (
      // The view fills its container and scrolls inside it, so it needs a
      // definite height rather than growing with the page.
      <div className="flex h-[calc(100vh-16rem)] min-h-[560px] flex-col">
        <LogsTools scope={{ userFilters, accountType, includeLocalTools }} />
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-3">
      <h3 className="text-lg font-medium">Tool logs</h3>
      {body}
    </div>
  );
}
