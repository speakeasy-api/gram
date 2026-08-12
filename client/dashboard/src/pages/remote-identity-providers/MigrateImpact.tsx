import { Alert } from "@/components/ui/Alert";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";

// MigrateImpact renders the server's authoritative preflight: what moves, what
// blocks the migration, and what changes without blocking it. Blockers are
// rendered as errors because the mutation rejects them; warnings are rendered as
// warnings because the target's values simply become authoritative.
//
// Shared by the organization consolidation dialog and the platform-admin
// convergence dialog. Both call preflight endpoints that return the same blocker
// set, and the two surfaces must not describe the same blocker differently: an
// admin who sees one wording in the tenant UI and another in the platform UI has
// no way to tell whether they are looking at the same problem.
export function MigrateImpact({
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
      <Text small muted>
        Checking impact…
      </Text>
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

  // An unresolved preflight is not a clean one. A query that is paused or
  // otherwise settled without data reports neither loading nor error, and
  // defaulting the count to zero there would render "0 clients move" with no
  // blockers, which reads as a safe migration on data nobody has.
  if (clientCount === undefined) {
    return (
      <Text small muted>
        Checking impact…
      </Text>
    );
  }

  const count = clientCount;

  return (
    <Stack gap={2}>
      <Text small muted>
        {count} {count === 1 ? "client moves" : "clients move"} to the target
        provider.
        {mcpServerNames && mcpServerNames.length > 0
          ? ` Affected MCP servers: ${mcpServerNames.join(", ")}.`
          : ""}
      </Text>

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
