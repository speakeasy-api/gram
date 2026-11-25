import type { Draft } from "./model";

// Must match maxAdminJSONBodyBytes in server/internal/admin/impl.go.
const maxRequestBytes = 1024 * 1024;

export function serializeSupportMatrixUpdate(
  revision: string,
  draft: Draft,
): string {
  const validateText = (text: string) => {
    if (text.includes("\0"))
      throw new Error("Notes and conditions must not contain NUL characters.");
  };
  for (const mapping of Object.values(draft.mappings)) {
    validateText(mapping.conditions);
    for (const fact of Object.values(mapping.facts)) validateText(fact.note);
  }
  for (const facts of Object.values(draft.references))
    for (const fact of Object.values(facts)) validateText(fact.note);
  const body = JSON.stringify({ revision, draft });
  if (new TextEncoder().encode(body).length > maxRequestBytes)
    throw new Error(
      "The complete support matrix exceeds the 1 MB save limit. Shorten notes or conditions before importing or saving.",
    );
  return body;
}
