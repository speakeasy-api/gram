import { GramError } from "@gram/client/models/errors/gramerror.js";

export function errorMessage(error: unknown, fallback: string): string {
  if (error instanceof Error && error.message) return error.message;
  return fallback;
}

/**
 * A step whose check the server cannot run yet answers 503. Matched on the base
 * error class, not ServiceError: the SDK only decodes a typed body for 4XX, 500
 * and 502, so a 503 arrives as whichever GramError it fell back to.
 */
export function isUnavailable(error: unknown): boolean {
  return error instanceof GramError && error.statusCode === 503;
}
