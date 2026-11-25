import type { Deployment } from "@gram/client/models/components/deployment.js";

export type RemovableSourceIdentity = {
  kind: "openapi" | "function";
  /** The deployment asset id the caller knows the source by. */
  assetId: string;
  /** Stable across versions, so it finds the source in other deployments. */
  slug?: string;
};

// Evolve clones the *latest* deployment, whichever id the form names. When a
// newer push failed after re-uploading this source, that deployment carries
// the source under fresh ids, and excluding only the active deployment's id
// would leave it in the clone. The exclusion list therefore names every id
// the source has, in every deployment it could be cloned from; the server
// drops an attachment when either its own id or its file's id is listed.
export function exclusionIdsForSource(
  source: RemovableSourceIdentity,
  deployments: Array<Deployment | undefined>,
): string[] {
  const ids = new Set<string>([source.assetId]);
  if (!source.slug) return [...ids];
  for (const deployment of deployments) {
    const attachments =
      source.kind === "openapi"
        ? (deployment?.openapiv3Assets ?? [])
        : (deployment?.functionsAssets ?? []);
    for (const attachment of attachments) {
      if (attachment.slug !== source.slug) continue;
      ids.add(attachment.id);
      ids.add(attachment.assetId);
    }
  }
  return [...ids];
}

/** Whether a deployment has stopped changing, one way or the other. */
export function isTerminalDeploymentStatus(status: string): boolean {
  return status === "completed" || status === "failed";
}
