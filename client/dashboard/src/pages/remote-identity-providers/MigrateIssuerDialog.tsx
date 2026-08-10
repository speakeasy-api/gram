import { Dialog } from "@/components/ui/Dialog";
import { Label } from "@/components/ui/Label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Text } from "@/components/ui/Text";
import type { OrganizationRemoteSessionIssuer } from "@gram/client/models/components/organizationremotesessionissuer.js";
import { useMigrateOrganizationRemoteSessionIssuerMutation } from "@gram/client/react-query/migrateOrganizationRemoteSessionIssuer.js";
import { useOrganizationRemoteSessionIssuerMigratePreflight } from "@gram/client/react-query/organizationRemoteSessionIssuerMigratePreflight.js";
import { invalidateAllOrganizationRemoteSessionIssuers } from "@gram/client/react-query/organizationRemoteSessionIssuers.js";
import { Button } from "@/components/ui/Button";
import { Stack } from "@/components/ui/Stack";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import { MigrateImpact } from "./MigrateImpact";
import { issuerDisplayName } from "./issuerDisplay";

// MigrateIssuerDialog consolidates one identity provider onto another that
// describes the same upstream authorization server. The source's clients are
// re-pointed at the target and the source is removed. Existing remote sessions
// travel with their clients, so nobody re-authenticates.
export function MigrateIssuerDialog({
  source,
  candidates,
  onClose,
  onMigrated,
}: {
  source: OrganizationRemoteSessionIssuer;
  candidates: OrganizationRemoteSessionIssuer[];
  onClose: () => void;
  onMigrated?: () => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const [targetId, setTargetId] = useState<string>("");

  const {
    data: preflight,
    isLoading: preflightLoading,
    isError: preflightFailed,
  } = useOrganizationRemoteSessionIssuerMigratePreflight(
    { sourceId: source.issuer.id, targetId },
    undefined,
    { enabled: targetId !== "" },
  );

  const migrate = useMigrateOrganizationRemoteSessionIssuerMutation({
    onSuccess: async () => {
      await invalidateAllOrganizationRemoteSessionIssuers(queryClient, {
        refetchType: "all",
      });
      toast.success("Providers consolidated");
      onMigrated?.();
      onClose();
    },
    onError: (error) => {
      toast.error(
        error instanceof Error
          ? error.message
          : "Failed to consolidate providers",
      );
    },
  });

  // Submit only on a preflight that came back clean. Treating a missing preflight
  // as unblocked would enable the button whenever the preflight request failed,
  // letting the admin fire a migration the server will reject with no on-screen
  // explanation of why.
  const canSubmit = preflight?.canMigrate === true && !migrate.isPending;

  const handleMigrate = () => {
    migrate.mutate({
      request: {
        migrateIssuerRequestBody: { sourceId: source.issuer.id, targetId },
      },
    });
  };

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>
            Consolidate "{issuerDisplayName(source.issuer)}"
          </Dialog.Title>
          <Dialog.Description>
            Move this provider's clients onto another provider for the same
            upstream identity provider, then remove this one. Existing sessions
            keep working, so nobody has to sign in again.
          </Dialog.Description>
        </Dialog.Header>

        <Stack gap={2}>
          <Label className="text-muted-foreground text-xs">
            Consolidate into
          </Label>
          {candidates.length === 0 ? (
            <Text small muted>
              No other provider in this organization can absorb this one. A
              target must be organizational, or belong to the same project.
            </Text>
          ) : (
            <Select value={targetId} onValueChange={setTargetId}>
              <SelectTrigger>
                <SelectValue placeholder="Select a provider" />
              </SelectTrigger>
              <SelectContent>
                {candidates.map((candidate) => (
                  <SelectItem
                    key={candidate.issuer.id}
                    value={candidate.issuer.id}
                  >
                    {issuerDisplayName(candidate.issuer)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
        </Stack>

        {targetId !== "" && (
          <MigrateImpact
            isLoading={preflightLoading}
            hasFailed={preflightFailed}
            clientCount={preflight?.clientCount}
            mcpServerNames={preflight?.mcpServerNames}
            endpointMismatches={preflight?.endpointMismatches}
            conflictingMcpServerNames={preflight?.conflictingMcpServerNames}
            warnings={preflight?.warnings}
          />
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
            onClick={handleMigrate}
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
