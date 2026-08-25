import { GramError } from "@gram/client/models/errors/gramerror.js";

// isNotNotFound is a `throwOnError` predicate for detail-page reads that treat
// a 404 as "return to the list" rather than as a crash: only a not-found is
// kept for the caller's own handling, and every other failure still reaches
// the nearest error boundary the way the default query config sends it.
export function isNotNotFound(error: unknown): boolean {
  return !(error instanceof GramError && error.statusCode === 404);
}
