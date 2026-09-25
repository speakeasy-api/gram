/**
 * The trailing wildcard terminator. A rule carries it explicitly so it states
 * its own breadth: a bare stem matches the same subjects while hiding that it
 * does.
 */
const WILDCARD_SUFFIX = "*";

export type MatchKind = "exact" | "wildcard";

/**
 * The subject a rule will be stored with, given the match kind and what the
 * operator typed.
 *
 * A wildcard rule's terminator is owned by the match kind rather than typed: the
 * dialog shows it as a fixed suffix, so selecting Wildcard is the only place
 * breadth is stated. Before this, an operator had to say it twice — pick Wildcard
 * *and* type the `*` — and the warnings below existed largely to police the two
 * disagreeing.
 *
 * An exact subject is passed through verbatim, including any `*`, so the warning
 * about a literal star still fires. Silently stripping it would hide the mistake
 * this function exists to surface.
 */
export function composeSubject(matchKind: MatchKind, typed: string): string {
  const trimmed = typed.trim();
  if (matchKind !== "wildcard" || trimmed.length === 0) {
    return trimmed;
  }
  return trimmed.endsWith(WILDCARD_SUFFIX)
    ? trimmed
    : trimmed + WILDCARD_SUFFIX;
}

/**
 * What the field should hold after switching to `next`.
 *
 * Switching to Wildcard lifts a typed terminator out of the field, so the suffix
 * the dialog renders does not end up doubled. Switching to Exact leaves the text
 * alone: the operator may have meant that star, and the warning will say so.
 */
export function typedSubjectForMatchKind(
  typed: string,
  next: MatchKind,
): string {
  if (next !== "wildcard") {
    return typed;
  }
  const trimmedEnd = typed.trimEnd();
  return trimmedEnd.endsWith(WILDCARD_SUFFIX)
    ? trimmedEnd.slice(0, -WILDCARD_SUFFIX.length)
    : typed;
}

/**
 * Whether typing this value should switch the match kind to wildcard.
 *
 * Pasting a rule copied from elsewhere is the case that matters: it arrives with
 * its terminator, and leaving the kind on Exact would store a literal star that
 * matches nothing. Only offered where the issuer permits wildcards, so this
 * cannot select a kind the server would refuse.
 */
export function shouldSwitchToWildcard(
  typed: string,
  current: MatchKind,
  wildcardPermitted: boolean,
): boolean {
  return (
    current === "exact" &&
    wildcardPermitted &&
    typed.trimEnd().endsWith(WILDCARD_SUFFIX) &&
    typed.trimEnd().length > WILDCARD_SUFFIX.length
  );
}

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
  // Trailing whitespace before the "*" is stored verbatim and matches nothing —
  // the same silent failure this function exists to catch, and invisible in the
  // field. Only the outer whitespace is trimmed, so this has to be checked.
  if (stem !== stem.trimEnd()) {
    return 'This rule ends in whitespace before its "*", which is stored as part of the subject and would match nothing.';
  }

  return null;
}

// Exported so the gate can be tested directly. Asserting it through the rendered
// button cannot distinguish "blocked by the warning" from "blocked because no
// agent is selected yet", so a test driven through the UI stays green even if
// the warning stops blocking the submit.
export function canAdmit(input: {
  issuer: string;
  subject: string;
  agentId: string;
  warning: string | null;
  /**
   * Whether the selected issuer URL still resolves to a row in the list. A
   * preselected single issuer, or one withdrawn in another tab between render
   * and submit, can leave a URL selected that no longer exists.
   */
  issuerExists: boolean;
  /**
   * Whether the rule as typed is permitted by that issuer. The picker disables
   * the wildcard option, but the selected match kind is held in state: switching
   * issuers, or the issuer's permission being cleared elsewhere, can leave a
   * wildcard selected under an issuer that forbids it.
   */
  matchKindPermitted: boolean;
}): boolean {
  return (
    input.issuer.length > 0 &&
    input.issuerExists &&
    input.matchKindPermitted &&
    input.subject.trim().length > 0 &&
    input.agentId.length > 0 &&
    input.warning === null
  );
}
