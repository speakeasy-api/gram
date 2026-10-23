import type { AdminUpstreamOAuthMigrationStatus } from "@gram/admin-client/models/components/adminupstreamoauthmigrationstatus";

// An organization needs a run while the migration is incomplete, its typed
// auth clients have work a run can do, a failed phase included, or assistant
// execution profiles have versions to record. Unresolved
// configuration needs the data fixed first and warnings need nothing, so they
// alone never count, nor do auth clients a run is already recording. A status
// whose auth clients could not be checked counts on the migration alone.
export function needsMigrationRun(
  status: AdminUpstreamOAuthMigrationStatus | undefined,
): boolean {
  if (status === undefined) return false;
  if (!status.migrated) return true;
  if (status.authClients?.inProgress) return false;
  if ((status.executionProfiles?.pendingVersions ?? 0) > 0) return true;
  const a = status.authClients;
  if (a === undefined || a.inProgress) return false;
  return (
    a.pendingClients +
      a.pendingLinks +
      a.pendingHeaders +
      a.pendingRetirements +
      a.pendingMarkedServers +
      (a.pendingSelectionMappings ?? 0) +
      a.failedPhases.length >
    0
  );
}
