import type { IssuerFieldMismatch } from "@gram/client/models/components/issuerfieldmismatch.js";

// The preflight and convergence endpoints report a consolidation difference as a
// field name plus both sides' values, and leave the wording to the client. These
// helpers are that wording, shared so the tenant and platform surfaces cannot
// describe the same difference differently.
//
// Fields are named in their wire spelling (token_endpoint, scopes_supported)
// rather than through a display-name table. The same spelling appears in the
// conflict error the migrate mutation returns and in the convergence listing's
// status column, so an admin who meets a difference twice meets one name for it.

// mismatchFieldNames lists the fields a mismatch set covers, for the summaries
// that name what disagrees without room to show the values.
export function mismatchFieldNames(
  mismatches: IssuerFieldMismatch[],
): string[] {
  return mismatches.map((mismatch) => mismatch.field);
}

// isListMismatch reports whether a mismatch describes a list-valued field, which
// the payload signals by carrying entries on at least one side.
//
// A list-valued field whose two sides are both empty is indistinguishable from a
// scalar whose two sides are both unset. Neither has a delta to draw, and the
// scalar form describes both honestly, so both fall through to it.
export function isListMismatch(mismatch: IssuerFieldMismatch): boolean {
  return (
    (mismatch.sourceValues?.length ?? 0) > 0 ||
    (mismatch.targetValues?.length ?? 0) > 0
  );
}

// mismatchValueLabel renders one side of a scalar difference. An absent value is
// spelled out rather than left blank: the server treats "declares nothing" and
// "declares an empty value" as different, and one of them blocks a migration, so
// a blank would read as a rendering fault rather than as the difference it is.
export function mismatchValueLabel(value: string | undefined): string {
  if (value === undefined) {
    return "not set";
  }

  if (value === "") {
    return "empty";
  }

  return value;
}

// listMismatchDelta splits a list-valued difference into what the migrated
// clients gain and what they lose. Both sides can come back empty — two lists
// that hold the same entries a different number of times differ without either
// side holding an entry the other lacks — and the caller falls back to showing
// the two lists whole.
export function listMismatchDelta(mismatch: IssuerFieldMismatch): {
  added: string[];
  removed: string[];
} {
  const source = mismatch.sourceValues ?? [];
  const target = mismatch.targetValues ?? [];

  return {
    added: target.filter((entry) => !source.includes(entry)),
    removed: source.filter((entry) => !target.includes(entry)),
  };
}

// warningSentence describes one non-blocking difference: what the target
// overwrites for every client that moves.
export function warningSentence(mismatch: IssuerFieldMismatch): string {
  if (isListMismatch(mismatch)) {
    return `${mismatch.field} differs; the target provider's values become authoritative.`;
  }

  return `${mismatch.field} changes from ${mismatchValueLabel(mismatch.sourceValue)} to ${mismatchValueLabel(mismatch.targetValue)} for migrated clients.`;
}
