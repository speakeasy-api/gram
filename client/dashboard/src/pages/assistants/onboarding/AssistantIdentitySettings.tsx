import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Text } from "@/components/ui/Text";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { useRBAC } from "@/hooks/useRBAC";
import { useOrgRoutes, useRoutes } from "@/routes";
import type { Assistant } from "@gram/client/models/components/assistant.js";
import { useAssistantsUpgradeIdentityMutation } from "@gram/client/react-query/assistantsUpgradeIdentity.js";
import { invalidateAllAssistantsList } from "@gram/client/react-query/assistantsList.js";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router";
import { toast } from "sonner";
import { useAgents } from "@gram/client/react-query/agents.js";
import { useAgent } from "@gram/client/react-query/agent.js";
import { useSlackDirectoryMembers } from "@gram/client/react-query/slackDirectoryMembers.js";
import { Bot } from "lucide-react";
import { SESSION_SECURITY } from "@/pages/org/identity-provider/identityProviderQueries";

export function AssistantIdentitySettings({
  assistant,
  onUpdated,
}: {
  assistant: Assistant;
  onUpdated?: () => void;
}): JSX.Element | null {
  const identityFlag = useFeatureFlag(FEATURE_FLAGS.agentCredentials);
  const { hasScope } = useRBAC();
  const queryClient = useQueryClient();
  const [confirming, setConfirming] = useState(false);
  const [selectedIdentity, setSelectedIdentity] = useState("new");
  const [agentName, setAgentName] = useState(assistant.name);
  const agentsQuery = useAgents({}, undefined, {
    enabled: confirming,
    throwOnError: false,
  });
  const agents = agentsQuery.data ?? [];
  const creating = selectedIdentity === "new";
  const nameTaken = agents.some(
    (agent) =>
      agent.name.toLocaleLowerCase() === agentName.trim().toLocaleLowerCase(),
  );
  const attributionName = creating
    ? agentName.trim()
    : agents.find((agent) => agent.id === selectedIdentity)?.name;
  const validSelection =
    !agentsQuery.isPending &&
    !agentsQuery.isError &&
    (creating ? !!agentName.trim() && !nameTaken : !!attributionName);

  const upgrade = useAssistantsUpgradeIdentityMutation({
    onSuccess: () => {
      setConfirming(false);
      void invalidateAllAssistantsList(queryClient);
      onUpdated?.();
      toast.success("Workloads set up");
    },
    onError: (error) => {
      if (error.message.includes("already uses this name")) {
        toast.error(
          "This identity name is already taken. Choose a different name.",
        );
        void agentsQuery.refetch();
        return;
      }
      toast.error(
        "Could not update identity. Refresh and check your permissions and identity health.",
      );
    },
  });
  const legacy = assistant.identityState === "NEVER_CONFIGURED";
  const canUpgrade =
    hasScope("project:write", assistant.projectId) &&
    identityFlag.status === "enabled" &&
    legacy;

  if (identityFlag.status !== "enabled") return null;

  if (!legacy) return <ConfiguredIdentity assistant={assistant} />;

  return (
    <div className="space-y-4">
      <Text small muted>
        This assistant uses legacy authentication bindings. Actions are
        attributed to the owner.
      </Text>
      {canUpgrade && (
        <Button size="sm" onClick={() => setConfirming(true)}>
          Set up workloads
        </Button>
      )}
      <Dialog open={confirming} onOpenChange={setConfirming}>
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>Set up workloads</Dialog.Title>
            <Dialog.Description>
              Choose the agent identity for {assistant.name}.
            </Dialog.Description>
          </Dialog.Header>
          <div className="space-y-4 py-2">
            <div className="space-y-2">
              <label
                htmlFor="assistant-agent-selection"
                className="text-sm font-medium"
              >
                Agent identity
              </label>
              <select
                id="assistant-agent-selection"
                value={selectedIdentity}
                onChange={(event) => setSelectedIdentity(event.target.value)}
                className="w-full rounded-md border bg-background px-3 py-2 text-sm"
                disabled={upgrade.isPending}
              >
                <option value="new">Create new</option>
                {agents.map((agent) => (
                  <option key={agent.id} value={agent.id}>
                    {agent.name}
                  </option>
                ))}
              </select>
            </div>
            {creating && (
              <div className="space-y-2">
                <label
                  htmlFor="assistant-agent-name"
                  className="text-sm font-medium"
                >
                  Identity name
                </label>
                <input
                  id="assistant-agent-name"
                  value={agentName}
                  onChange={(event) => setAgentName(event.target.value)}
                  maxLength={120}
                  className="w-full rounded-md border bg-background px-3 py-2 text-sm"
                  disabled={upgrade.isPending}
                  aria-invalid={nameTaken}
                  aria-describedby={
                    nameTaken ? "assistant-agent-name-error" : undefined
                  }
                />
                {nameTaken && (
                  <p
                    id="assistant-agent-name-error"
                    className="text-sm text-destructive"
                  >
                    An identity already uses this name. Choose a different name
                    or select the existing identity.
                  </p>
                )}
              </div>
            )}
            {agentsQuery.isError && (
              <Text small muted>
                Could not load agent identities. Close this dialog and try
                again.
              </Text>
            )}
            {attributionName && (
              <div className="flex flex-wrap items-center gap-2 rounded-lg bg-muted/50 px-3 py-3 text-sm text-muted-foreground">
                <span>Actions will be attributed to</span>
                <span className="inline-flex items-center gap-1.5 rounded-md border bg-background px-2 py-1 font-medium text-foreground">
                  <Bot aria-hidden="true" className="size-4" />
                  {attributionName}
                </span>
              </div>
            )}
          </div>
          <Dialog.Footer>
            <Button
              variant="tertiary"
              disabled={upgrade.isPending}
              onClick={() => setConfirming(false)}
            >
              Cancel
            </Button>
            <Button
              disabled={upgrade.isPending || !canUpgrade || !validSelection}
              onClick={() =>
                upgrade.mutate({
                  request: {
                    upgradeAssistantIdentityRequestBody: {
                      id: assistant.id,
                      ...(creating
                        ? { agentName: agentName.trim() }
                        : { agentId: selectedIdentity }),
                    },
                  },
                })
              }
            >
              {upgrade.isPending ? "Setting up…" : "Confirm setup"}
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </div>
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
            Link assistant permissions to the user prompting it:
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
