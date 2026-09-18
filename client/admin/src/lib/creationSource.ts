// How an organization came to exist, as recorded by the flow that created it.
// Informational for operators: nothing about trials, entitlements or onboarding
// reads it.

// The prospect flow starts with a platform admin creating the organization from
// the admin app. Named once, because the label map and the record both ask
// about this value and a second spelling would let them disagree.
export const PLATFORM_ADMIN_SOURCE = "platform_admin";

// The server writes these strings verbatim, so the map is keyed on them rather
// than on a union the SDK would have to keep in step. A source this list does
// not know is shown as it arrived instead of being hidden or renamed, because a
// flow added on the server is still true about the organization.
const CREATION_SOURCE_LABELS: Record<string, string> = {
  [PLATFORM_ADMIN_SOURCE]: "Platform admin",
  signup: "Self-serve signup",
  assistants: "Assistants",
};

// What the record shows when nothing recorded a source: every organization
// created before the field existed, and every one first written by WorkOS
// organization sync. Neither is evidence of any particular flow, so the wording
// says only that.
export const CREATION_SOURCE_UNRECORDED = "Not recorded";

export type CreationSourceFact = {
  // What the record shows beside "Created via".
  label: string;

  // Whether a flow recorded anything at all. The record draws an unrecorded
  // source in muted text, so this has to agree with `label` for every input —
  // hence one function rather than a label helper the caller pairs with its own
  // truthiness test, which disagreed for a blank string.
  recorded: boolean;

  // Whether this is the platform-admin prospect flow, which the record calls
  // out beside the label.
  platformAdmin: boolean;
};

export function creationSourceFact(
  source: string | undefined,
): CreationSourceFact {
  const trimmed = source?.trim();
  if (!trimmed) {
    return {
      label: CREATION_SOURCE_UNRECORDED,
      recorded: false,
      platformAdmin: false,
    };
  }

  return {
    label: CREATION_SOURCE_LABELS[trimmed] ?? trimmed,
    recorded: true,
    platformAdmin: trimmed === PLATFORM_ADMIN_SOURCE,
  };
}
