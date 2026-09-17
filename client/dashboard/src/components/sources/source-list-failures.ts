import { useMemo } from "react";
import { sourceFailureKey } from "./source-list-filters";
import { useFailedDeploymentSources } from "./useFailedDeploymentSources";

export interface SourceFailures {
  /** `sourceFailureKey`s of the sources the latest deployment's logs blame. */
  failingKeys: ReadonlySet<string>;
  /** Set when the latest deployment failed, whichever source is to blame. */
  failedDeploymentId: string | undefined;
}

/**
 * Which sources broke the latest push.
 *
 * The list shows the active deployment, but the failure lives on the latest
 * one, which may be a newer push that never became active. The hook reads
 * that deployment's logs and reduces them to keys the list can look up.
 */
export function useSourceFailures(): SourceFailures {
  const { hasFailures, failedSources, deployment } =
    useFailedDeploymentSources();

  return useMemo(
    () => ({
      // Catalog servers can fail a deployment too, but they aren't on this
      // shelf, so only the two kinds it lists are keyed.
      failingKeys: new Set(
        failedSources
          .filter(
            (failed) => failed.type === "openapi" || failed.type === "function",
          )
          .map((failed) =>
            sourceFailureKey({
              kind: failed.type === "function" ? "function" : "openapi",
              slug: failed.slug,
            }),
          ),
      ),
      failedDeploymentId: hasFailures ? deployment?.id : undefined,
    }),
    [hasFailures, failedSources, deployment],
  );
}
