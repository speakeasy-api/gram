import type { AdminUpstreamOAuthMigrationStatus } from "@gram/admin-client/models/components/adminupstreamoauthmigrationstatus";

// An organization needs a run while the migration is incomplete or its typed
// auth clients have work a run can do, a failed phase included. Unresolved
// configuration needs the data fixed first and warnings need nothing, so they
// alone never count, nor do auth clients a run is already recording. A status
// whose auth clients could not be checked counts on the migration alone.
export function needsMigrationRun(
  status: AdminUpstreamOAuthMigrationStatus | undefined,
): boolean {
  if (status === undefined) return false;
  if (!status.migrated) return true;
  const a = status.authClients;
  if (a === undefined || a.inProgress) return false;
  return (
    a.pendingClients +
      a.pendingLinks +
      a.pendingHeaders +
      a.pendingRetirements +
      a.pendingMarkedServers +
      a.failedPhases.length >
    0
  );
}
