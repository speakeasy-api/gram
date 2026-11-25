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
import type { WorkloadIssuer } from "@gram/client/models/components/workloadissuer.js";
import { invalidateAllWorkloadIdentities } from "@gram/client/react-query/workloadIdentities.js";
import { useWithdrawWorkloadSubjectMutation } from "@gram/client/react-query/withdrawWorkloadSubject.js";
import { useQueryClient } from "@tanstack/react-query";
import { useAgentAdmissions } from "./agentAdmissions";
import { useState } from "react";
import { Link } from "react-router";
import { toast } from "sonner";
import { useRoutes } from "@/routes";

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
  const routes = useRoutes();
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

  const mine = identities.admissions;
  const rows = federatedAccessRows(mine, identities.issuers);

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
          rows.map((row) =>
            row.kind === "runtime" ? (
              <IdentityPanelRow
                key={row.issuer.id}
                title="Assistant runtime"
                // Each running sandbox gets its own sign-in when it starts and
                // loses it when it stops, so they are counted, not listed.
                detail={`${row.count} running ${row.count === 1 ? "sandbox" : "sandboxes"}`}
                trailing={
                  row.showPolicy ? (
                    <WorkloadIssuerPolicy issuer={row.issuer} />
                  ) : undefined
                }
              />
            ) : (
              <IdentityPanelRow
                key={row.admission.id}
                title={row.admission.issuerName || row.admission.issuer}
                // The subject is how Speakeasy recognises the platform's tokens.
                // Nobody reading this page has to know it, so the row says what
                // the rule covers instead of printing it.
                detail={enumPresentation(
                  {
                    wildcard: "Any workload in the connected organization",
                    exact: "One workload",
                  },
                  row.admission.matchKind,
                  "Unknown workload coverage",
                )}
                // A wildcard rule the platform has not enabled admits nothing.
                // That is indistinguishable from working until a sign-in fails.
                accent={
                  row.admission.matchKind === "wildcard" &&
                  !row.admission.wildcardActive
                    ? "warning"
                    : undefined
                }
                trailing={
                  <div className="flex items-center gap-2">
                    {row.issuer && <WorkloadIssuerPolicy issuer={row.issuer} />}
                    {/* The setup flow's Agent sign-in step, which reads the
                        rule back and replaces it when it changes. */}
                    {agent.permissions.write && (
                      <Button asChild variant="secondary" size="sm">
                        <Link
                          to={`${routes.identities.agents.edit.href(agent.id)}?step=3`}
                          aria-label={`Change ${row.admission.issuerName || row.admission.issuer} access`}
                        >
                          Change
                        </Link>
                      </Button>
                    )}
                    <RequireScope scope="workload:write" level="component">
                      <Button
                        variant="destructive-secondary"
                        size="sm"
                        disabled={withdraw.isPending}
                        aria-label={`Remove ${row.admission.issuerName || row.admission.issuer} access`}
                        onClick={() => setRemoving(row.admission)}
                      >
                        <Button.Text>Remove</Button.Text>
                      </Button>
                    </RequireScope>
                  </div>
                }
              />
            ),
          )
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

type FederatedAccessRow =
  | {
      kind: "runtime";
      issuer: WorkloadIssuer;
      // The issuer's policy control shows once, on its first row.
      showPolicy: boolean;
      count: number;
    }
  | {
      kind: "admission";
      admission: WorkloadAdmission;
      // The issuer's policy control shows once, on its first row.
      issuer: WorkloadIssuer | undefined;
    };

/**
 * Whether Speakeasy created this admission for one of its own assistant
 * sandboxes. Tags and names can be edited, so this requires every marker the
 * server checks before it treats an issuer as its own, plus the admission
 * name it gives each sandbox. Anything else keeps its own row and Remove.
 */
function isRuntimeAdmission(
  admission: WorkloadAdmission,
  issuer: WorkloadIssuer,
): boolean {
  return (
    /^kit-runtime-[0-9a-f]{32}$/.test(issuer.name) &&
    issuer.tags.length === 1 &&
    issuer.tags[0] === "kit-runtime" &&
    issuer.allowAssertionReuse &&
    !issuer.allowWildcardAdmission &&
    admission.matchKind === "exact" &&
    /^kit-runtime-[0-9a-f-]{36}-[0-9a-f]{32}$/.test(admission.name)
  );
}

/**
 * One row per admission, except that an assistant's sandboxes collapse into a
 * single row per issuer. A busy assistant holds one admission per running
 * sandbox, and listing each one buried the rest of the panel.
 */
function federatedAccessRows(
  admissions: WorkloadAdmission[],
  issuers: WorkloadIssuer[],
): FederatedAccessRow[] {
  const issuerByID = new Map(issuers.map((issuer) => [issuer.id, issuer]));
  const rows: FederatedAccessRow[] = [];
  const runtimeRows = new Map<string, { count: number }>();
  const issuersShown = new Set<string>();
  for (const admission of admissions) {
    const issuer = issuerByID.get(admission.workloadIssuerId);
    if (issuer && isRuntimeAdmission(admission, issuer)) {
      const existing = runtimeRows.get(issuer.id);
      if (existing) {
        existing.count += 1;
      } else {
        const row = {
          kind: "runtime" as const,
          issuer,
          showPolicy: !issuersShown.has(issuer.id),
          count: 1,
        };
        issuersShown.add(issuer.id);
        runtimeRows.set(issuer.id, row);
        rows.push(row);
      }
      continue;
    }
    const first = issuer !== undefined && !issuersShown.has(issuer.id);
    if (issuer) issuersShown.add(issuer.id);
    rows.push({
      kind: "admission",
      admission,
      issuer: first ? issuer : undefined,
    });
  }
  return rows;
}
