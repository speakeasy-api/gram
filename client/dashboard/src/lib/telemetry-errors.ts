import { GramError } from "@gram/client/models/errors/gramerror";

/**
 * Detects if a 403 error is specifically because logs are disabled.
 * Other 403 errors (e.g., permission denied, invalid API key) will return false.
 */
export function isLogsDisabledError(error: unknown): boolean {
  if (error instanceof GramError && error.statusCode === 403) {
    return error.message.includes("logs are not enabled");
  }
  return false;
}
