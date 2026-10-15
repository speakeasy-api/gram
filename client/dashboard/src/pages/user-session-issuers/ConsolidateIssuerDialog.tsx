import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Label } from "@/components/ui/Label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import type { OrganizationUserSessionIssuerMigratePreflight } from "@gram/client/models/components/organizationusersessionissuermigratepreflight.js";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { useMigrateOrganizationUserSessionIssuerMutation } from "@gram/client/react-query/migrateOrganizationUserSessionIssuer.js";
import { useOrganizationUserSessionIssuerMigratePreflight } from "@gram/client/react-query/organizationUserSessionIssuerMigratePreflight.js";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import { invalidateIssuers } from "./invalidateIssuers";
import { changesIssuerUrl, describeMismatch } from "./issuerDisplay";

/**
 * Consolidates one Speakeasy authorization server into another: its clients,
 * sessions, and MCP servers move to the target, and it is removed. The
 * server's preflight decides what blocks the move and which settings change;
 * confirming here confirms exactly those changes.
 */
export function ConsolidateIssuerDialog({
  source,
  candidates,
  candidatesLoading = false,
  onRetryCandidates,
  onClose,
  onConsolidated,
}: {
  source: UserSessionIssuer;
  candidates: UserSessionIssuer[];
  /** Still loading candidates: say so rather than that there are none. */
  candidatesLoading?: boolean;
  /**
   * Set when candidates failed to load. The list may be partial, so nothing
   * is offered until a retry loads it all.
   */
  onRetryCandidates?: () => void;
  onClose: () => void;
  /** Called with the surviving authorization server. */
  onConsolidated?: (target: UserSessionIssuer) => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const [targetId, setTargetId] = useState("");

  const preflight = useOrganizationUserSessionIssuerMigratePreflight(
    { sourceId: source.id, targetId },
    undefined,
    { enabled: targetId !== "", throwOnError: false, retry: false },
  );

  const migrate = useMigrateOrganizationUserSessionIssuerMutation({
    onSuccess: async (result) => {
      toast.success(`Consolidated ${source.slug} into ${result.issuer.slug}`);
      // Leave first: refreshing while the source's page is still open would
      // refetch it, and it no longer exists.
      onConsolidated?.(result.issuer);
      onClose();
      await invalidateIssuers(queryClient);
    },
    onError: () => {
      // The settings may have changed since the preview; show the current one.
      void preflight.refetch();
    },
  });

  // Only a preview that is current can be confirmed: not one being refreshed,
  // and not one whose refresh failed.
  const current =
    preflight.isFetching || preflight.isError ? undefined : preflight.data;
  const canSubmit = current?.canMigrate === true && !migrate.isPending;

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !migrate.isPending) onClose();
      }}
    >
      {/* Wide enough for two issuer URLs side by side in a warning. */}
      <Dialog.Content className="max-w-2xl">
        <Dialog.Header>
          <Dialog.Title>Consolidate {source.slug}</Dialog.Title>
          <Dialog.Description>
            Move this authorization server's clients, sessions, and MCP servers
            to another one, then remove it. Existing sessions keep working.
          </Dialog.Description>
        </Dialog.Header>

        <Stack gap={2}>
          <Label className="text-muted-foreground text-xs">
            Consolidate into
          </Label>
          {candidatesLoading ? (
            <Text small muted>
              Loading authorization servers…
            </Text>
          ) : onRetryCandidates ? (
            <Stack direction="horizontal" align="center" gap={3}>
              <Text small className="text-destructive">
                Couldn't load every authorization server.
              </Text>
              <Button variant="secondary" size="sm" onClick={onRetryCandidates}>
                <Button.Text>Retry</Button.Text>
              </Button>
            </Stack>
          ) : candidates.length === 0 ? (
            <Text small muted>
              No other authorization server can take this one's place. The
              target must be organization-wide, or in the same project.
            </Text>
          ) : (
            <Select
              value={targetId}
              onValueChange={(value) => {
                migrate.reset();
                setTargetId(value);
              }}
              disabled={migrate.isPending}
            >
              <SelectTrigger aria-label="Consolidate into" className="w-full">
                <SelectValue placeholder="Select an authorization server" />
              </SelectTrigger>
              <SelectContent>
                {candidates.map((candidate) => (
                  <SelectItem key={candidate.id} value={candidate.id}>
                    {candidate.slug}
                    {candidate.projectName ? ` · ${candidate.projectName}` : ""}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
        </Stack>

        {targetId !== "" && (
          <ConsolidateImpact
            preflight={current}
            isLoading={preflight.isFetching}
            hasFailed={preflight.isError}
            onRetry={() => void preflight.refetch()}
          />
        )}

        {migrate.isError && (
          <Alert variant="error" dismissible={false}>
            {migrate.error instanceof GramError &&
            migrate.error.statusCode === 409
              ? "Something changed since this preview. Review it again before consolidating."
              : "Couldn't consolidate. Try again."}
          </Alert>
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
            disabled={!canSubmit}
            onClick={() =>
              current &&
              migrate.mutate({
                request: {
                  migrateIssuerRequestBody2: {
                    sourceId: source.id,
                    targetId,
                    // Confirms the warnings shown above, exactly.
                    confirmedWarningsFingerprint:
                      current.warningsFingerprint || undefined,
                  },
                },
              })
            }
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

function plural(count: number, one: string, many: string): string {
  return `${count} ${count === 1 ? one : many}`;
}

/** Everything consolidating moves; the rarer kinds only when there are any. */
function moves(
  preflight: OrganizationUserSessionIssuerMigratePreflight,
): string {
  const parts = [
    plural(preflight.clientCount, "client", "clients"),
    plural(preflight.sessionCount, "session", "sessions"),
    preflight.consentCount > 0 &&
      plural(preflight.consentCount, "sign-in approval", "sign-in approvals"),
    preflight.cimdClientCount > 0 &&
      plural(
        preflight.cimdClientCount,
        "allowed client document",
        "allowed client documents",
      ),
    preflight.remoteSessionCount > 0 &&
      plural(preflight.remoteSessionCount, "linked account", "linked accounts"),
  ].filter((part): part is string => typeof part === "string");
  return parts.length === 1
    ? parts[0]!
    : `${parts.slice(0, -1).join(", ")} and ${parts.at(-1)}`;
}

/** What the server says consolidating moves, blocks, and changes. */
function ConsolidateImpact({
  preflight,
  isLoading,
  hasFailed,
  onRetry,
}: {
  preflight: OrganizationUserSessionIssuerMigratePreflight | undefined;
  isLoading: boolean;
  hasFailed: boolean;
  onRetry: () => void;
}): JSX.Element {
  if (hasFailed && !isLoading) {
    return (
      <Alert variant="error" dismissible={false}>
        <Stack direction="horizontal" align="center" gap={3}>
          <span className="text-sm">
            Couldn't check what consolidating changes.
          </span>
          <Button variant="secondary" size="sm" onClick={onRetry}>
            <Button.Text>Retry</Button.Text>
          </Button>
        </Stack>
      </Alert>
    );
  }
  if (isLoading || !preflight) {
    return (
      <Text small muted>
        Checking what changes…
      </Text>
    );
  }

  const blockers = [
    preflight.platformOwned &&
      "One of these is managed by the Speakeasy Platform MCP, so it can't be consolidated. Choose another target.",
    preflight.conflictingClientIds.length > 0 &&
      `${plural(preflight.conflictingClientIds.length, "client is", "clients are")} registered with both (${preflight.conflictingClientIds.join(", ")}). Revoke the copy on one of them in its MCP server's Clients and Sessions, then try again.`,
    preflight.principalBindingConflictCount > 0 &&
      `${plural(preflight.principalBindingConflictCount, "person is", "people are")} linked to an identity provider account through both. Contact Speakeasy support to consolidate these.`,
    preflight.emaBindingConflictCount > 0 &&
      `The target already has ${plural(preflight.emaBindingConflictCount, "enterprise-managed authorization link", "enterprise-managed authorization links")}. Contact Speakeasy support to consolidate these.`,
  ].filter((blocker): blocker is string => typeof blocker === "string");

  return (
    <Stack gap={3}>
      <Text small muted>
        {moves(preflight)} move to the target.
      </Text>

      {blockers.length > 0 && (
        <Alert variant="error" dismissible={false}>
          <ul className="list-disc space-y-1 pl-4">
            {blockers.map((blocker) => (
              <li key={blocker}>{blocker}</li>
            ))}
          </ul>
        </Alert>
      )}

      {preflight.warnings.length > 0 && (
        <Alert variant="warning" dismissible={false}>
          <p className="mb-2 text-sm">
            The target's settings apply from now on.
            {changesIssuerUrl(preflight.warnings) &&
              " Clients are sent to a different issuer URL, so update any identity provider or platform set up with the current one."}
          </p>
          <ul className="space-y-1">
            {preflight.warnings.map((warning) => {
              const { label, from, to } = describeMismatch(warning);
              return (
                <li key={warning.field} className="text-sm break-all">
                  <span className="font-medium">{label}:</span> {from} → {to}
                </li>
              );
            })}
          </ul>
        </Alert>
      )}
    </Stack>
  );
}
