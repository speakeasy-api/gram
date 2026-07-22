import { Dialog } from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Type } from "@/components/ui/type";
import type { OrganizationRemoteSessionIssuer } from "@gram/client/models/components/organizationremotesessionissuer.js";
import { useMigrateOrganizationRemoteSessionIssuerMutation } from "@gram/client/react-query/migrateOrganizationRemoteSessionIssuer.js";
import { useOrganizationRemoteSessionIssuerMigratePreflight } from "@gram/client/react-query/organizationRemoteSessionIssuerMigratePreflight.js";
import { invalidateAllOrganizationRemoteSessionIssuers } from "@gram/client/react-query/organizationRemoteSessionIssuers.js";
import { Alert, Button, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
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
            <Type small muted>
              No other provider in this organization can absorb this one. A
              target must be organizational, or belong to the same project.
            </Type>
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

// MigrateImpact renders the server's authoritative preflight: what moves, what
// blocks the migration, and what changes without blocking it. Blockers are
// rendered as errors because the mutation rejects them; warnings are rendered as
// warnings because the target's values simply become authoritative.
function MigrateImpact({
  isLoading,
  hasFailed,
  clientCount,
  mcpServerNames,
  endpointMismatches,
  conflictingMcpServerNames,
  warnings,
}: {
  isLoading: boolean;
  hasFailed: boolean;
  clientCount: number | undefined;
  mcpServerNames: string[] | undefined;
  endpointMismatches: string[] | undefined;
  conflictingMcpServerNames: string[] | undefined;
  warnings: string[] | undefined;
}): JSX.Element {
  if (isLoading) {
    return (
      <Type small muted>
        Checking impact…
      </Type>
    );
  }

  // Without a preflight there is nothing trustworthy to show. Say so rather than
  // rendering a zero impact summary that reads like a clean migration.
  if (hasFailed) {
    return (
      <Alert variant="error" dismissible={false}>
        Could not check the impact of this migration. Try again.
      </Alert>
    );
  }

  const count = clientCount ?? 0;

  return (
    <Stack gap={2}>
      <Type small muted>
        {count} {count === 1 ? "client moves" : "clients move"} to the target
        provider.
        {mcpServerNames && mcpServerNames.length > 0
          ? ` Affected MCP servers: ${mcpServerNames.join(", ")}.`
          : ""}
      </Type>

      {endpointMismatches && endpointMismatches.length > 0 && (
        <Alert variant="error" dismissible={false}>
          {`These providers describe different authorization servers (${endpointMismatches.join(", ")} differ). Consolidating them would break existing sessions.`}
        </Alert>
      )}

      {conflictingMcpServerNames && conflictingMcpServerNames.length > 0 && (
        <Alert variant="error" dismissible={false}>
          {`Both providers already have a client on these MCP servers: ${conflictingMcpServerNames.join(", ")}. Remove one client per server, then try again.`}
        </Alert>
      )}

      {warnings && warnings.length > 0 && (
        <Alert variant="warning" dismissible={false}>
          {warnings.join(" ")}
        </Alert>
      )}
    </Stack>
  );
}
