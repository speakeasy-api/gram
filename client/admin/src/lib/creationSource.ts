// How an organization came to exist, as recorded by the flow that created it.
// Informational for operators: nothing about trials, entitlements or onboarding
// reads it.
//
// The server writes these strings verbatim, so the map is keyed on them rather
// than on a union the SDK would have to keep in step. A source this list does
// not know is shown as it arrived instead of being hidden or renamed, because a
// flow added on the server is still true about the organization.
const CREATION_SOURCE_LABELS: Record<string, string> = {
  platform_admin: "Platform admin",
  signup: "Self-serve signup",
  assistants: "Assistants",
};

// What the record shows when nothing recorded a source: every organization
// created before the field existed, and every one first written by WorkOS
// organization sync. Neither is evidence of any particular flow, so the wording
// says only that.
export const CREATION_SOURCE_UNRECORDED = "Not recorded";

export function creationSourceLabel(source: string | undefined): string {
  const trimmed = source?.trim();
  if (!trimmed) return CREATION_SOURCE_UNRECORDED;
  return CREATION_SOURCE_LABELS[trimmed] ?? trimmed;
}

// Whether the organization was created by a platform admin, which is how the
// prospect flow starts. The record says so in words next to the label; this is
// the one place that decides what "so" means.
export function isPlatformAdminCreated(source: string | undefined): boolean {
  return source?.trim() === "platform_admin";
}
