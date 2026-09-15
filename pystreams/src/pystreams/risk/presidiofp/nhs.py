"""UK NHS number false-positive catalogs.

Mirror of ``server/internal/risk/presidiofp/nhs.go``; see ``non_nhs_reason`` and
``nhs_context_reason`` for the rationale behind each layer.
"""


def non_nhs_reason(match: str) -> str:
    """Return why a UK_NHS match is noise, or "" when it could be a real NHS number.

    This layer sees only the matched value, so it can rule out digit runs that
    are not valid NHS numbers at all. The much larger noise class — a perfectly
    valid-looking 10-digit run that is really a Confluence page id, a Unix
    timestamp, or an order number — is indistinguishable by value alone and is
    handled by ``nhs_context_reason`` instead.

    Why the value-only layer matters: Presidio's ``NhsRecognizer`` matches
    ``\\b(\\d{3})[- ]?(\\d{3})[- ]?(\\d{4})\\b`` and, when its mod-11 checksum
    passes, ``PatternRecognizer`` raises the score to 1.0 with no context
    requirement. Any 10-digit identifier therefore has a ~1-in-11 chance of being
    reported as a UK National Health Service number at maximum confidence.

    Three checks, in order:

      1. Shape. Anything that is not exactly ten digits after separators are
         stripped is left alone: it did not come from the NHS recognizer's own
         grammar, so this catalog has nothing to say about it.
      2. Check digit. NHS numbers carry a mod-11 check digit (the same
         validation the recognizer runs). A run that fails it is not an NHS
         number.
      3. Allocation range. NHS/CHI/H&C/IHI numbers are only issued from the
         ranges in ``NHS_ALLOCATED_RANGES``. A checksum-valid run outside them
         was never issued to a patient.
    """
    digits = _nhs_digits(match)
    if len(digits) != 10:
        return ""
    if not _nhs_check_digit_valid(digits):
        return "fails the NHS number check digit"
    if not _nhs_allocated(digits):
        return "outside the allocated NHS number ranges"
    return ""


def nhs_context_reason(text: str) -> str:
    """Report a UK_NHS match as noise when its text carries no NHS signal at all.

    The recognizer already ships CONTEXT words ("nhs", "national health
    service", ...) but Presidio only uses them to *raise* a score, never to gate
    a match, and a passing checksum pins the score at 1.0 before any context is
    consulted. This inverts that: a ten-digit run in a payload that never
    mentions health care anywhere is treated as an opaque identifier rather than
    a patient number.

    ``text`` is the whole scanned payload, not a window around the match, so
    that this agrees with the offline sweep, which re-locates a stored match
    inside re-fetched message text and cannot rely on offsets. Scanning
    everything only ever keeps more findings, which is the safe direction.

    An empty ``text`` means "context unknown", and no finding is suppressed on
    that basis.
    """
    if not text:
        return ""
    lower = text.lower()
    if any(term in lower for term in NHS_CONTEXT_TERMS):
        return ""
    return "ten-digit identifier with no NHS context in the surrounding text"


def _nhs_digits(match: str) -> str:
    """Strip the separators the NHS recognizer's grammar allows (spaces and
    hyphens, per its ``replacement_pairs``) and return the remaining characters
    only when every one of them is a digit. Anything else returns "" so callers
    treat the value as out of scope rather than mis-measuring it.
    """
    out: list[str] = []
    for ch in match.strip():
        if ch in " -":
            continue
        if not ch.isascii() or not ch.isdigit():
            return ""
        out.append(ch)
    return "".join(out)


def _nhs_check_digit_valid(digits: str) -> bool:
    """Run the NHS Digital mod-11 check: each of the ten digits is weighted 10
    down to 1 and the weighted sum must be divisible by 11. Same validation as
    Presidio's ``NhsRecognizer``, reimplemented so a stored finding can be
    re-checked offline without calling the analyzer.
    """
    total = sum(int(ch) * (10 - i) for i, ch in enumerate(digits))
    return total % 11 == 0


def _nhs_allocated(digits: str) -> bool:
    """Report whether the run's leading nine digits fall in an issued range."""
    prefix = int(digits[:9])
    return any(low <= prefix <= high for low, high in NHS_ALLOCATED_RANGES)


# Inclusive ranges over the first nine digits of an NHS number (the tenth is the
# check digit) that identify a real person somewhere in the UK/Ireland numbering
# scheme. Everything outside them has never been issued, so a checksum-valid run
# there is an unrelated identifier that happens to validate.
#
# Sourced from the NHS Data Dictionary (HEALTH AND CARE NUMBER), NHS England's
# NHS-number guidance and the UK FCI NHS number range table:
#
#   010 000 000 - 319 999 999  Scotland (CHI numbers, DDMMYY-prefixed)
#   320 000 000 - 399 999 999  Northern Ireland (Health & Care numbers)
#   400 000 000 - 499 999 999  England, Wales, Isle of Man
#   600 000 000 - 799 999 999  England, Wales, Isle of Man
#   800 000 000 - 859 999 999  Republic of Ireland (IHI)
#
# Two gaps are deliberate. 000 000 000 - 009 999 999 is unissued, and it is where
# zero-padded internal ids land. 860 000 000 - 999 999 999 is unissued too; it
# contains NHS England's 999-prefixed test range, which is valid by checksum but
# reserved so it can never belong to a patient.
NHS_ALLOCATED_RANGES: tuple[tuple[int, int], ...] = (
    (10_000_000, 319_999_999),
    (320_000_000, 399_999_999),
    (400_000_000, 499_999_999),
    (600_000_000, 799_999_999),
    (800_000_000, 859_999_999),
)

# Lowercased substrings that count as NHS signal in the surrounding text. The
# list starts from the recognizer's own CONTEXT words and adds the sibling
# schemes that share the ten-digit format, so a Scottish CHI or Northern Irish
# Health & Care number is not suppressed for lacking the letters "nhs".
#
# Substring matching is intentional: "nhs" also fires inside "nhsNumber" and
# "NHS_NUMBER", the shapes these values take in JSON payloads and column names,
# which a word-boundary match would miss.
NHS_CONTEXT_TERMS: tuple[str, ...] = (
    "nhs",
    "national health service",
    "health service",
    "health authority",
    "health and care number",
    "health & care number",
    "chi number",
    "patient",
    "medical record",
    "hospital number",
)
