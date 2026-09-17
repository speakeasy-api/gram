import type { Draft } from "./model";

// Must match maxAdminJSONBodyBytes in server/internal/admin/impl.go.
const maxRequestBytes = 1024 * 1024;

export function serializeSupportMatrixUpdate(
  revision: string,
  draft: Draft,
): string {
  const body = JSON.stringify({ revision, draft });
  if (new TextEncoder().encode(body).length > maxRequestBytes)
    throw new Error(
      "The complete support matrix exceeds the 1 MB save limit. Shorten notes or conditions before importing or saving.",
    );
  return body;
}
