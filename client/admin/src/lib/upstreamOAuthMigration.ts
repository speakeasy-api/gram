import type { AdminUpstreamOAuthMigrationStatus } from "@gram/admin-client/models/components/adminupstreamoauthmigrationstatus";

// An organization needs a run while the migration is incomplete or its typed
// auth clients have work a run can do. Unresolved associations need the data
// fixed first, so they alone never count. A status whose auth clients could
// not be checked counts on the migration alone.
export function needsMigrationRun(
  status: AdminUpstreamOAuthMigrationStatus | undefined,
): boolean {
  if (status === undefined) return false;
  if (!status.migrated) return true;
  const a = status.authClients;
  if (a === undefined) return false;
  return (
    a.pendingClients +
      a.pendingLinks +
      a.pendingRetirements +
      a.pendingMarkedServers >
    0
  );
}
