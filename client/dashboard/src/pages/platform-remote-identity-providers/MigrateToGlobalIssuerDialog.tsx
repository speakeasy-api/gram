import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Text } from "@/components/ui/Text";
import type { IssuerConvergenceCandidate } from "@gram/client/models/components/issuerconvergencecandidate.js";
import { useMigrateToGlobalRemoteSessionIssuerMutation } from "@gram/client/react-query/migrateToGlobalRemoteSessionIssuer.js";
import { invalidateAllGlobalRemoteSessionIssuerConvergenceCandidates } from "@gram/client/react-query/globalRemoteSessionIssuerConvergenceCandidates.js";
import { invalidateAllGlobalRemoteSessionIssuer } from "@gram/client/react-query/globalRemoteSessionIssuer.js";
import { invalidateAllGlobalRemoteSessionIssuers } from "@gram/client/react-query/globalRemoteSessionIssuers.js";
import { useGlobalRemoteSessionIssuerMigratePreflight } from "@gram/client/react-query/globalRemoteSessionIssuerMigratePreflight.js";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { MigrateImpact } from "../remote-identity-providers/MigrateImpact";
import { issuerDisplayName } from "../remote-identity-providers/issuerDisplay";
import { candidateOwnerLabel } from "./convergenceBlockers";

// MigrateToGlobalIssuerDialog folds one organization's identity provider onto the
// platform catalog entry for the same upstream. The target is fixed by the page
// the admin came from, so unlike the tenant consolidation dialog there is
// nothing to pick — the candidate was already chosen from the list.
export function MigrateToGlobalIssuerDialog({
  candidate,
  targetId,
  targetName,
  onClose,
}: {
  candidate: IssuerConvergenceCandidate;
  targetId: string;
  targetName: string;
  onClose: () => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const sourceId = candidate.issuer.id;

  const {
    data: preflight,
    isLoading: preflightLoading,
    isError: preflightFailed,
  } = useGlobalRemoteSessionIssuerMigratePreflight({ sourceId, targetId });

  const migrate = useMigrateToGlobalRemoteSessionIssuerMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllGlobalRemoteSessionIssuerConvergenceCandidates(
          queryClient,
          {
            refetchType: "all",
          },
        ),
        // The migrated clients land on the platform issuer as tenant-owned, so
        // its client counts — and therefore whether it can still be deleted —
        // change as a result of this mutation. Both the catalog listing and the
        // single-issuer read carry those counts, and the detail page this dialog
        // opens from uses the singular one: invalidating only the list would
        // leave the Settings tab showing a stale count and a delete warning that
        // no longer matches reality.
        invalidateAllGlobalRemoteSessionIssuers(queryClient, {
          refetchType: "all",
        }),
        invalidateAllGlobalRemoteSessionIssuer(queryClient, {
          refetchType: "all",
        }),
      ]);
      toast.success("Provider consolidated onto the platform catalog");
      onClose();
    },
    onError: (error) => {
      toast.error(
        error instanceof Error
          ? error.message
          : "Failed to consolidate provider",
      );
    },
  });

  // Submit only on a preflight that came back clean. Treating a missing
  // preflight as unblocked would enable the button whenever the preflight
  // request failed, letting the admin fire a migration the server will reject
  // with no on-screen explanation of why.
  const canSubmit = preflight?.canMigrate === true && !migrate.isPending;

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      {/* Wider than a default dialog, and unconditionally so. The impact
      summary can carry two endpoint URLs per differing field, which wrap into
      an unreadable stack at the default width. Sizing it only when a blocker is
      present would resize the dialog under the admin as they change the target
      in the picker above. */}
      <Dialog.Content className="max-w-2xl">
        <Dialog.Header>
          <Dialog.Title>
            Consolidate "{issuerDisplayName(candidate.issuer)}"
          </Dialog.Title>
          <Dialog.Description>
            Owned by {candidateOwnerLabel(candidate)}. Move this provider's
            clients onto the platform provider "{targetName}", then remove the
            original. Existing sessions keep working, so nobody has to sign in
            again.
          </Dialog.Description>
        </Dialog.Header>

        <MigrateImpact
          isLoading={preflightLoading}
          hasFailed={preflightFailed}
          clientCount={preflight?.clientCount}
          mcpServerNames={preflight?.mcpServerNames}
          endpointMismatches={preflight?.endpointMismatches}
          conflictingMcpServerNames={preflight?.conflictingMcpServerNames}
          warnings={preflight?.warnings}
        />

        {preflight !== undefined && preflight.targetTenantClientCount > 0 && (
          <Text small muted>
            {preflight.targetTenantClientCount}{" "}
            {preflight.targetTenantClientCount === 1
              ? "organization-owned client is"
              : "organization-owned clients are"}{" "}
            already registered with "{targetName}".
          </Text>
        )}

        <Dialog.Footer>
          <Button
            variant="tertiary"
            onClick={onClose}
            disabled={migrate.isPending}
          >
            <Button.Text>Cancel</Button.Text>
          </Button>
          <Button
            variant="primary"
            onClick={() =>
              migrate.mutate({
                request: {
                  migrateRemoteSessionIssuerRequestBody: {
                    sourceId,
                    targetId,
                  },
                },
              })
            }
            disabled={!canSubmit}
          >
            <Button.Text>
              {migrate.isPending ? "Consolidating…" : "Consolidate"}
            </Button.Text>
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
