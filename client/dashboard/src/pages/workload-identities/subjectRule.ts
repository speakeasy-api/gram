/**
 * The trailing wildcard terminator. A rule carries it explicitly so it states
 * its own breadth: a bare stem matches the same subjects while hiding that it
 * does.
 */
const WILDCARD_SUFFIX = "*";

export type MatchKind = "exact" | "wildcard";

/**
 * The match kind a subject states, read off the value itself.
 *
 * There is no separate control for this. An operator writes the rule they mean
 * and the terminator says how broad it is — which is what the stored value has
 * always meant, so nothing is inferred that was not already there.
 *
 * Lossless, because an exact subject may never contain a `*`: ValidateSubjectRule
 * refuses one outright, so there is no legitimate exact value this could
 * misread. A `*` anywhere therefore means a wildcard was intended, and a
 * misplaced one is reported as a misplaced terminator rather than as a literal
 * star, which is the more useful of the two messages.
 */
export function inferMatchKind(subject: string): MatchKind {
  return subject.trim().includes(WILDCARD_SUFFIX) ? "wildcard" : "exact";
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
   * Whether the kind this subject states is permitted by the selected issuer.
   * An issuer that forbids wildcards is a state the operator can reach — an
   * older row, or one cleared during an incident — so a rule stating one has to
   * be refused here rather than only by the server.
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
