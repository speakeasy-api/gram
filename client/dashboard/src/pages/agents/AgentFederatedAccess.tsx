import { WorkloadIssuerPolicy } from "./WorkloadIssuerPolicy";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { enumPresentation } from "@/lib/enum";
import {
  IdentityPanel,
  IdentityPanelEmpty,
  IdentityPanelRow,
} from "@/pages/identities/IdentityPanel";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import type { WorkloadAdmission } from "@gram/client/models/components/workloadadmission.js";
import { invalidateAllWorkloadIdentities } from "@gram/client/react-query/workloadIdentities.js";
import { useWithdrawWorkloadSubjectMutation } from "@gram/client/react-query/withdrawWorkloadSubject.js";
import { useQueryClient } from "@tanstack/react-query";
import { useAgentAdmissions } from "./agentAdmissions";
import { isDeviceAdmission } from "./devices/device-enrollment";
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
  const identities = useAgentAdmissions(agent.id);
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

  // Enrolled devices are listed with the bootstrap tokens that enrolled them.
  const mine = identities.admissions.filter(
    (admission) => !isDeviceAdmission(admission),
  );
  const firstAdmissionByIssuer = new Map<string, string>();
  for (const admission of mine) {
    if (!firstAdmissionByIssuer.has(admission.workloadIssuerId)) {
      firstAdmissionByIssuer.set(admission.workloadIssuerId, admission.id);
    }
  }

  return (
    <>
      <IdentityPanel
        title="Federated access"
        loading={identities.isPending}
        error={identities.isError}
        onRetry={identities.refetch}
        loadingRows={1}
        footer={
          mine.length === 0
            ? undefined
            : "Each platform signs its own short-lived tokens, so no key is minted. Speakeasy trades them for a session as this agent."
        }
      >
        {mine.length === 0 ? (
          <IdentityPanelEmpty>
            No platform signs in as this agent. A platform that already mints
            tokens for its workloads can sign in as it, with no key to mint or
            store.
          </IdentityPanelEmpty>
        ) : (
          mine.map((admission) => (
            <IdentityPanelRow
              key={admission.id}
              title={admission.issuerName || admission.issuer}
              // The subject is how Speakeasy recognises the platform's tokens.
              // Nobody reading this page has to know it, so the row says what
              // the rule covers instead of printing it.
              detail={enumPresentation(
                {
                  wildcard: "Any workload in the connected organization",
                  exact: "One workload",
                },
                admission.matchKind,
                "Unknown workload coverage",
              )}
              // A wildcard rule the platform has not enabled admits nothing.
              // That is indistinguishable from working until a sign-in fails.
              accent={
                admission.matchKind === "wildcard" && !admission.wildcardActive
                  ? "warning"
                  : undefined
              }
              trailing={
                <div className="flex items-center gap-2">
                  {identities.issuers
                    .filter(
                      (issuer) =>
                        issuer.id === admission.workloadIssuerId &&
                        firstAdmissionByIssuer.get(issuer.id) === admission.id,
                    )
                    .map((issuer) => (
                      <WorkloadIssuerPolicy key={issuer.id} issuer={issuer} />
                    ))}
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
                </div>
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
