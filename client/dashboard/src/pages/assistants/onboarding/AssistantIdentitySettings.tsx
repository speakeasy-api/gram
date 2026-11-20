import { Text } from "@/components/ui/Text";
import { useRBAC } from "@/hooks/useRBAC";
import { useOrgRoutes, useRoutes } from "@/routes";
import type { Assistant } from "@gram/client/models/components/assistant.js";
import { Link } from "react-router";
import { useAgent } from "@gram/client/react-query/agent.js";
import { useSlackDirectoryMembers } from "@gram/client/react-query/slackDirectoryMembers.js";
import { Bot } from "lucide-react";
import { SESSION_SECURITY } from "@/pages/org/identity-provider/identityProviderQueries";

export function AssistantIdentitySettings({
  assistant,
}: {
  assistant: Assistant;
}): JSX.Element {
  if (assistant.identityState !== "NEVER_CONFIGURED")
    return <ConfiguredIdentity assistant={assistant} />;

  return (
    <Text small muted>
      This assistant has no agent identity. It acts as the person who messages
      it in the dashboard, and as its creator everywhere else.
    </Text>
  );
}

function ConfiguredIdentity({ assistant }: { assistant: Assistant }) {
  const routes = useRoutes();
  const orgRoutes = useOrgRoutes();
  const { hasScope } = useRBAC();
  const canManageMappings = hasScope("org:admin");
  const agent = useAgent({ id: assistant.agentId ?? "" }, SESSION_SECURITY, {
    enabled: !!assistant.agentId,
    retry: false,
    throwOnError: false,
  });
  const mappings = useSlackDirectoryMembers(
    { mappingStatus: "mapped", limit: 1 },
    SESSION_SECURITY,
    { enabled: canManageMappings, retry: false, throwOnError: false },
  );

  return (
    <div className="space-y-6">
      {assistant.agentId && (
        <Link
          className="inline-flex items-center gap-2 text-sm font-medium underline underline-offset-4"
          to={routes.identities.detail.overview.href(
            encodeURIComponent(`agent:${assistant.agentId}`),
          )}
        >
          <Bot className="size-4 text-muted-foreground" aria-hidden="true" />
          {agent.data?.name ?? "Agent identity"}
        </Link>
      )}
      {canManageMappings && mappings.data?.members.length === 0 && (
        <div className="space-y-2">
          <Text small muted>
            Map Slack members to people in your organization so the assistant
            uses the connected accounts of whoever messages it in Slack:
          </Text>
          <Link
            className="text-sm font-medium underline underline-offset-4"
            to={`${orgRoutes.identity.href()}?tab=slack-workspaces&slack_view=members`}
          >
            Set up Slack mapping
          </Link>
        </div>
      )}
    </div>
  );
}
