import type { SlackDirectoryMember } from "@gram/client/models/components/slackdirectorymember.js";

const findings: Record<string, string> = {
  member_became_bot: "Slack account became a bot or app",
  member_type_unknown: "Slack account type became unknown",
  member_deactivated: "Slack account was deactivated",
  member_unknown: "Slack account state became unknown",
  member_absent: "Not seen in a complete sync",
  email_changed: "Slack email changed after confirmation",
};
const normalizeEmail = (email?: string | null) =>
  (email ?? "").trim().toLowerCase();

/** True when a Slack email and a person's email are both known and differ. */
export function emailsDiffer(
  slackEmail?: string | null,
  personEmail?: string | null,
): boolean {
  const slack = normalizeEmail(slackEmail);
  const person = normalizeEmail(personEmail);
  return slack !== "" && person !== "" && slack !== person;
}

export function mappingFinding(
  member: SlackDirectoryMember,
): string | undefined {
  if (member.mappingConflictReason)
    return (
      findings[member.mappingConflictReason] ??
      "Directory evidence changed after confirmation"
    );
  if (member.mapping && !member.mapping.active)
    return "Mapped person is no longer active in this organization";
  return undefined;
}
