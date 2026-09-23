/**
 * The trailing wildcard terminator. A rule carries it explicitly so it states
 * its own breadth: a bare stem matches the same subjects while hiding that it
 * does.
 */
export const WILDCARD_SUFFIX = "*";

export type MatchKind = "exact" | "wildcard";

/**
 * Advisory problems with a subject as typed. The server refuses all of these;
 * surfacing them next to the field is what stops an operator submitting a rule
 * that is accepted into the table and then matches nothing.
 */
export function subjectRuleWarning(
  matchKind: MatchKind,
  subject: string,
): string | null {
  const trimmed = subject.trim();
  if (trimmed.length === 0) {
    return null;
  }

  if (matchKind === "exact") {
    if (trimmed.includes(WILDCARD_SUFFIX)) {
      // The one that reads as correct afterwards: an exact subject is compared
      // in full, so the "*" is a literal character and the rule admits nothing.
      return 'An exact subject is matched in full, literally, including the "*". This rule would admit nothing. Switch the match to Wildcard to cover every subject beginning with this value.';
    }
    return null;
  }

  if (!trimmed.endsWith(WILDCARD_SUFFIX)) {
    return 'A wildcard subject must end in "*", so the rule says how broad it is.';
  }

  const stem = trimmed.slice(0, -WILDCARD_SUFFIX.length);
  if (stem.length === 0) {
    return 'A bare "*" would admit every subject this issuer signs, including any other tenant of it.';
  }
  if (stem.includes(WILDCARD_SUFFIX)) {
    return 'Only a trailing "*" is allowed. An interior one would leave the middle of the subject open.';
  }

  return null;
}
