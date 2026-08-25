import { isUnauthorizedError } from "@/lib/route-errors";
import { GramError } from "@gram/client/models/errors/gramerror.js";

// isNotNotFound is a `throwOnError` predicate for detail-page reads that treat
// a 404 as "return to the list" rather than as a crash. It keeps the dashboard
// default's behavior for auth states (401 and 403 are handled by the cache and
// the RBAC gates, not by the error boundary) and adds the not-found case; every
// other failure still reaches the nearest error boundary.
export function isNotNotFound(error: unknown): boolean {
  if (isUnauthorizedError(error)) return false;
  return !(
    error instanceof GramError &&
    (error.statusCode === 403 || error.statusCode === 404)
  );
}
