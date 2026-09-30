import { getHttpStatusCode } from "@/lib/route-errors";

/**
 * Rotation failures the customer can act on. The server's own message names
 * internals ("published MCP packages", "previous key fate"), so it is never
 * shown; the status carries enough to pick the next step. Every case here left
 * the project unchanged — a rotation that half-completed reports itself on the
 * success path instead.
 */
export function rotationErrorCopy(error: unknown): string {
  switch (getHttpStatusCode(error) ?? 0) {
    case 412:
      return "Publish your marketplace package before creating a new observability key. Nothing was changed.";
    case 400:
      return "Turn on the Observability plugin for this project before creating a new key. Nothing was changed.";
    case 403:
      return "You need to be an organization admin to create a new observability key.";
    default:
      return "The new key could not be created. Nothing was changed — try again.";
  }
}
