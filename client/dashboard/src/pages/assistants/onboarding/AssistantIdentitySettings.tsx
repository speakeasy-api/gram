import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Text } from "@/components/ui/Text";
import { useRBAC } from "@/hooks/useRBAC";
import { useRoutes } from "@/routes";
import type { Assistant } from "@gram/client/models/components/assistant.js";
import { useAssistantsUpgradeIdentityMutation } from "@gram/client/react-query/assistantsUpgradeIdentity.js";
import { invalidateAllAssistantsList } from "@gram/client/react-query/assistantsList.js";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router";
import { toast } from "sonner";
import { Row, Section } from "./PanelSection";

const healthLabels: Record<string, string> = {
  legacy: "Legacy credentials",
  ready: "Configured",
  suspended: "Agent suspended",
  unavailable: "Identity unavailable",
};

export function AssistantIdentitySettings({
  assistant,
  onUpdated,
}: {
  assistant: Assistant;
  onUpdated?: () => void;
}): JSX.Element {
  const diagnostics = assistant.identityDiagnostics;
  const { hasScope } = useRBAC();
  const routes = useRoutes();
  const queryClient = useQueryClient();
  const [confirming, setConfirming] = useState(false);
  const upgrade = useAssistantsUpgradeIdentityMutation({
    onSuccess: (result) => {
      setConfirming(false);
      void invalidateAllAssistantsList(queryClient);
      onUpdated?.();
      const messages = {
        upgraded: "Assistant identity upgraded",
        repaired: "Missing identity bindings repaired",
        unchanged: "Assistant identity already configured; no changes made",
      };
      toast.success(
        result.identityUpgradeOutcome
          ? messages[result.identityUpgradeOutcome]
          : "Assistant identity updated",
      );
    },
    onError: () => {
      toast.error(
        "Could not update identity. Refresh and check your permissions and identity health.",
      );
    },
  });
  const legacy = assistant.identityState === "NEVER_CONFIGURED";
  const missingRoots = diagnostics?.bindings.some(
    (binding) =>
      binding.state === "missing" && binding.triggerStatus === "active",
  );
  // A bounded diagnostic page cannot prove that all active roots exist.
  // The repair endpoint safely skips existing bindings.
  const repairable =
    diagnostics?.health === "ready" &&
    (missingRoots || diagnostics.bindingsTruncated);
  const canUpgrade =
    hasScope("project:write") &&
    diagnostics?.provisioningEnabled &&
    (legacy || repairable);
  const action = legacy ? "Upgrade identity" : "Repair missing bindings";

  return (
    <Section
      title="Workload identity"
      action={
        canUpgrade ? (
          <Button
            variant="tertiary"
            size="sm"
            onClick={() => setConfirming(true)}
          >
            {action}
          </Button>
        ) : undefined
      }
    >
      {!diagnostics ? (
        <Text small muted>
          Identity diagnostics are unavailable. Refresh to try again.
        </Text>
      ) : (
        <>
          <Row label="Health">
            <Text small>{healthLabels[diagnostics.health] ?? "Unknown"}</Text>
          </Row>
          {assistant.agentId && (
            <Row label="Acting agent">
              <Link
                className="text-xs underline"
                to={routes.identities.detail.overview.href(
                  encodeURIComponent(`agent:${assistant.agentId}`),
                )}
              >
                View agent and access
              </Link>
            </Row>
          )}
          <Row label="Binding generation">
            <Text small>
              {assistant.identityGeneration ?? "Not configured"}
            </Text>
          </Row>
          <Row label="New identities">
            <Text small>
              {diagnostics.provisioningEnabled
                ? "Enabled"
                : "Disabled by rollout"}
            </Text>
          </Row>
          <Row label="Workload execution">
            <Text small>
              {diagnostics.executionEnabled ? "Enabled" : "Disabled by rollout"}
            </Text>
          </Row>
          <Row label="Slack delegation">
            <Text small>
              {diagnostics.slackDelegationEnabled
                ? "Enabled"
                : "Disabled by rollout"}
            </Text>
          </Row>
          <div className="mt-2 space-y-1">
            {diagnostics.bindings.map((binding) => (
              <div
                key={binding.triggerId}
                className="rounded border border-border px-2 py-1 text-xs"
              >
                <div className="flex justify-between gap-2">
                  <span>
                    {binding.triggerKind} · {binding.triggerStatus}
                  </span>
                  <span>
                    {binding.state} · generation {binding.generation}
                  </span>
                </div>
                <div className="break-all text-muted-foreground">
                  {binding.triggerId}
                </div>
              </div>
            ))}
            {diagnostics.bindingsTruncated && (
              <Text small muted>
                Showing the first 100 trigger roots.
              </Text>
            )}
          </div>
          {diagnostics.lastExecutionMode && (
            <div className="mt-3">
              <Row label="Last execution">
                <Text small>{diagnostics.lastExecutionMode}</Text>
              </Row>
              <Row label="Event status">
                <Text small>{diagnostics.lastEventStatus}</Text>
              </Row>
              {diagnostics.lastFallbackReason && (
                <Row label="Selection reason">
                  <Text small>{diagnostics.lastFallbackReason}</Text>
                </Row>
              )}
              {diagnostics.lastInitiatingUserId && (
                <Row label="Delegating user">
                  <Text small>{diagnostics.lastInitiatingUserId}</Text>
                </Row>
              )}
            </div>
          )}
          <Text small muted className="mt-3">
            The agent acts for each message. A resolved user narrows business
            access; otherwise execution is autonomous. Thread history and
            replies are shared.
          </Text>
          <Text small muted className="mt-2">
            Configured identity and user mapping do not grant business
            permissions or OAuth consent. Existing consent is required;
            execution-aware external OAuth is not enabled.
          </Text>
          {diagnostics.health === "suspended" && (
            <Text small muted className="mt-2">
              Token issuance is denied while the agent is suspended. The
              assistant is not paused and never falls back to legacy
              credentials.
            </Text>
          )}
          {diagnostics.health === "unavailable" && (
            <Text small muted className="mt-2">
              Use existing agent and workload controls to inspect withdrawn
              authority. Repair does not restore revoked identities.
            </Text>
          )}
        </>
      )}
      <Dialog open={confirming} onOpenChange={setConfirming}>
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>{action}</Dialog.Title>
            <Dialog.Description>
              Confirm this exact assistant and project. This is an explicit
              identity change, not a permission grant or OAuth consent.
            </Dialog.Description>
          </Dialog.Header>
          <div className="space-y-2 break-all text-sm">
            <p>{assistant.name}</p>
            <p>Assistant: {assistant.id}</p>
            <p>Project: {assistant.projectId}</p>
          </div>
          <Text small muted>
            Existing bindings and revocation history are preserved. Missing live
            roots can be provisioned, but revoked authority cannot be restored.
          </Text>
          <Dialog.Footer>
            <Button
              variant="tertiary"
              disabled={upgrade.isPending}
              onClick={() => setConfirming(false)}
            >
              Cancel
            </Button>
            <Button
              disabled={upgrade.isPending || !canUpgrade}
              onClick={() =>
                upgrade.mutate({
                  request: { riskIDRequestBody: { id: assistant.id } },
                })
              }
            >
              {upgrade.isPending ? "Updating…" : "Confirm identity change"}
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </Section>
  );
}
