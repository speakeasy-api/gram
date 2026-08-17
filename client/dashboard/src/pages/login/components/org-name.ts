/**
 * Client-side mirror of the server's `validateOrgName`
 * (server/internal/auth/org_name.go), shared by the sign-up and register
 * panels so the two cannot drift.
 *
 * The server is authoritative — it re-validates and re-normalizes whatever
 * arrives — so this exists to give the field an error before the round trip,
 * which on the sign-up path is a top-level navigation the panel cannot catch.
 */

/** Matches the server's cap, counted in code points rather than UTF-16 units. */
export const MAX_ORG_NAME_LENGTH = 100;

/**
 * Floor on letters and digits, keeping out names made only of punctuation or
 * symbols. Two rather than one so a name is a name rather than an initial.
 */
const MIN_ORG_NAME_LETTERS_OR_DIGITS = 2;

export const ORG_NAME_REQUIRED_MESSAGE = "Company name is required";
export const ORG_NAME_TOO_LONG_MESSAGE = `Company name must be ${MAX_ORG_NAME_LENGTH} characters or fewer`;
export const ORG_NAME_INVALID_MESSAGE =
  "Company name contains characters that can't be displayed";
export const ORG_NAME_TOO_SHORT_MESSAGE = `Company name must contain at least ${MIN_ORG_NAME_LETTERS_OR_DIGITS} letters or numbers`;

/**
 * Every character the server rejects: control characters, bidi overrides and
 * other formatting codes, private-use and surrogate code points, and unassigned
 * ones — the complement of Go's `unicode.IsGraphic`. The zero-width joiner and
 * non-joiner are formatting codes too, but Indic, Arabic and Persian
 * orthography needs them, so both sides let them through.
 */
const NON_GRAPHIC_REGEX = /[^\p{L}\p{M}\p{N}\p{P}\p{S}\p{Zs}\u200c\u200d]/u;

const LETTER_OR_DIGIT_REGEX = /[\p{L}\p{N}]/gu;

/**
 * Collapses Unicode space separators and trims ASCII spaces. Other whitespace
 * remains intact so validation can reject controls just like the server does.
 */
export function normalizeOrgName(value: string): string {
  return value.replace(/\p{Zs}+/gu, " ").replace(/^ +| +$/gu, "");
}

/** The error to show for `value`, or undefined when it is acceptable. */
export function validateOrgName(value: string): string | undefined {
  const normalized = normalizeOrgName(value);
  if (!normalized) return ORG_NAME_REQUIRED_MESSAGE;

  // Array.from rather than `.length`: a name outside the Basic Multilingual
  // Plane spends two UTF-16 units per character, and the server counts
  // characters. Not grapheme clusters — the server counts runes, so a code
  // point is the unit that matches.
  if (Array.from(normalized).length > MAX_ORG_NAME_LENGTH) {
    return ORG_NAME_TOO_LONG_MESSAGE;
  }

  if (NON_GRAPHIC_REGEX.test(normalized)) return ORG_NAME_INVALID_MESSAGE;

  const lettersOrDigits = normalized.match(LETTER_OR_DIGIT_REGEX)?.length ?? 0;
  if (lettersOrDigits < MIN_ORG_NAME_LETTERS_OR_DIGITS) {
    return ORG_NAME_TOO_SHORT_MESSAGE;
  }

  return undefined;
}
