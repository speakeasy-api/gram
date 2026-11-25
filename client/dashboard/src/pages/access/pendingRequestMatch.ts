import type { AccessRequest } from "@gram/client/models/components/accessrequest.js";
import type { ChallengeBucket } from "@gram/client/models/components/challengebucket.js";

/**
 * How far before a request a denial can be and still be the one it asks
 * about. Matches the server's look-back when an approval resolves denials.
 */
const REQUEST_LOOKBACK_MS = 7 * 24 * 60 * 60 * 1000;

/**
 * The pending access request a denied bucket answers, if any: asked by the
 * denied member for the same scope, on a resource its selector covers, for a
 * denial from the week before the request onwards. This follows the rule the
 * server uses to resolve denials when the request is approved
 * (`openChallengesForRequest`), compared on resource kind and id only: the
 * server also checks the selector's other dimensions, so this is a label for
 * the admin, not a promise. The newest request wins when several match.
 */
export function pendingRequestForBucket(
  bucket: ChallengeBucket,
  requests: readonly AccessRequest[],
): AccessRequest | undefined {
  if (bucket.outcome !== "deny" || bucket.resolvedAt) return undefined;
  if (bucket.principalType !== "user") return undefined;
  let match: AccessRequest | undefined;
  for (const request of requests) {
    if (request.status !== "pending") continue;
    if (bucket.principalUrn !== `user:${request.requesterUserId}`) continue;
    if (bucket.scope !== request.scope) continue;
    if (!selectorCovers(request, bucket)) continue;
    if (
      bucket.lastSeen.getTime() <
      request.createdAt.getTime() - REQUEST_LOOKBACK_MS
    ) {
      continue;
    }
    if (!match || request.createdAt > match.createdAt) match = request;
  }
  return match;
}

function selectorCovers(
  request: AccessRequest,
  bucket: ChallengeBucket,
): boolean {
  const { resourceKind, resourceId } = request.selector;
  if (
    resourceKind !== "*" &&
    bucket.resourceKind &&
    resourceKind !== bucket.resourceKind
  ) {
    return false;
  }
  return resourceId === "*" || resourceId === bucket.resourceId;
}
