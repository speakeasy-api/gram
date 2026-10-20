import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import {
  IdentityPanel,
  IdentityPanelEmpty,
  IdentityPanelRow,
} from "@/pages/identities/IdentityPanel";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import type { WorkloadAdmission } from "@gram/client/models/components/workloadadmission.js";
import {
  invalidateAllWorkloadIdentities,
  useWorkloadIdentities,
} from "@gram/client/react-query/workloadIdentities.js";
import { useWithdrawWorkloadSubjectMutation } from "@gram/client/react-query/withdrawWorkloadSubject.js";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";

/**
 * The platforms whose own tokens this agent accepts, and the way back out.
 *
 * An admission names an agent, so the list is this agent's by filtering on
 * that id rather than by asking the server for a slice it does not offer.
 */
export function AgentFederatedAccess({
  agent,
}: {
  agent: ManagedAgent;
}): JSX.Element {
  const queryClient = useQueryClient();
  const identities = useWorkloadIdentities({});
  const [removing, setRemoving] = useState<WorkloadAdmission | null>(null);

  const withdraw = useWithdrawWorkloadSubjectMutation({
    onSuccess: async () => {
      await invalidateAllWorkloadIdentities(queryClient, {
        refetchType: "all",
      });
      setRemoving(null);
      toast.success("Access removed");
    },
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : "Could not remove the access",
      );
    },
  });

  const mine = (identities.data?.admissions ?? []).filter(
    (admission) => admission.agentId === agent.id,
  );

  return (
    <>
      <IdentityPanel
        title="Federated access"
        loading={identities.isPending}
        error={identities.isError}
        onRetry={() => void identities.refetch()}
        loadingRows={1}
        footer={
          mine.length === 0
            ? undefined
            : "Each platform signs its own tokens. Speakeasy trades them for a session as this agent."
        }
      >
        {mine.length === 0 ? (
          <IdentityPanelEmpty>
            No platform signs in as this agent. Agents reached this way carry no
            key of their own.
          </IdentityPanelEmpty>
        ) : (
          mine.map((admission) => (
            <IdentityPanelRow
              key={admission.id}
              title={admission.issuerName || admission.issuer}
              // The subject is how Speakeasy recognises the platform's tokens.
              // Nobody reading this page has to know it, so the row says what
              // the rule covers instead of printing it.
              detail={
                admission.matchKind === "wildcard"
                  ? "Any workload in the connected organization"
                  : "One workload"
              }
              // A wildcard rule the platform has not enabled admits nothing.
              // That is indistinguishable from working until a sign-in fails.
              accent={
                admission.matchKind === "wildcard" && !admission.wildcardActive
                  ? "warning"
                  : undefined
              }
              trailing={
                <RequireScope scope="workload:write" level="component">
                  <Button
                    variant="destructive-secondary"
                    size="sm"
                    disabled={withdraw.isPending}
                    aria-label={`Remove ${admission.issuerName || admission.issuer} access`}
                    onClick={() => setRemoving(admission)}
                  >
                    <Button.Text>Remove</Button.Text>
                  </Button>
                </RequireScope>
              }
            />
          ))
        )}
      </IdentityPanel>

      <Dialog
        open={removing !== null}
        onOpenChange={(next) => {
          if (!next && withdraw.isPending) return;
          if (!next) setRemoving(null);
        }}
      >
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>
              Remove {removing?.issuerName || "this platform"}&apos;s access?
            </Dialog.Title>
            <Dialog.Description>
              It can no longer sign in as {agent.name}. Sessions it already
              holds are not revoked, and you can set it up again at any time.
            </Dialog.Description>
          </Dialog.Header>
          <Dialog.Footer>
            <Button
              variant="secondary"
              disabled={withdraw.isPending}
              onClick={() => setRemoving(null)}
            >
              <Button.Text>Cancel</Button.Text>
            </Button>
            <Button
              variant="destructive-primary"
              disabled={withdraw.isPending}
              onClick={() => {
                if (removing) withdraw.mutate({ request: { id: removing.id } });
              }}
            >
              <Button.Text>
                {withdraw.isPending ? "Removing…" : "Remove access"}
              </Button.Text>
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </>
  );
}
