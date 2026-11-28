import { adminApplyPluginRepositoryMigrations } from "@gram/admin-client/funcs/adminApplyPluginRepositoryMigrations";
import { adminListPluginRepositoryMigrations } from "@gram/admin-client/funcs/adminListPluginRepositoryMigrations";
import { adminReprovisionPluginRepository } from "@gram/admin-client/funcs/adminReprovisionPluginRepository";
import type { AdminPluginRepositoryMigrationList } from "@gram/admin-client/models/components/adminpluginrepositorymigrationlist";
import type { AdminPluginRepositoryMigrationOutcome } from "@gram/admin-client/models/components/adminpluginrepositorymigrationoutcome";
import type { AdminPluginRepositoryReprovisionResult } from "@gram/admin-client/models/components/adminpluginrepositoryreprovisionresult";
import { unwrapAsync } from "@gram/admin-client/types/fp";
import { redirecting, redirectingClient } from "@/lib/adminSdkClient";
import type { RepositoryEvidence } from "@/lib/pluginRepositoryEvidence";

export function listPluginRepositoryMigrations(
  after?: string,
  signal?: AbortSignal,
): Promise<AdminPluginRepositoryMigrationList> {
  return redirecting(
    unwrapAsync(
      adminListPluginRepositoryMigrations(
        redirectingClient(),
        { after },
        { signal },
      ),
    ),
  );
}

export async function applyPluginRepositoryMigrations(
  entries: RepositoryEvidence[],
  dryRun: boolean,
): Promise<AdminPluginRepositoryMigrationOutcome[]> {
  const { items } = await redirecting(
    unwrapAsync(
      adminApplyPluginRepositoryMigrations(
        redirectingClient(),
        { entries, dryRun },
        { retries: { strategy: "none" } },
      ),
    ),
  );
  return items;
}

export function reprovisionPluginRepository(
  projectId: string,
  reason: string,
): Promise<AdminPluginRepositoryReprovisionResult> {
  return redirecting(
    unwrapAsync(
      adminReprovisionPluginRepository(
        redirectingClient(),
        { projectId, reason },
        { retries: { strategy: "none" } },
      ),
    ),
  );
}
