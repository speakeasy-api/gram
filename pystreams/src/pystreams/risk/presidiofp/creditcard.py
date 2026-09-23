"""Payment-card false-positive catalogs.

Mirror of ``server/internal/risk/presidiofp/creditcard.go``; see
``non_card_reason`` and ``card_context_reason`` for the rationale behind each
layer.
"""

import itertools

# Bounds of the digit runs Presidio's ``CreditCardRecognizer`` can emit. Its
# pattern is a 4-digit leading group followed by three more groups of 3-5 digits,
# so the shortest match is 13 digits and the longest 19 — which also happens to
# be the ISO/IEC 7812 PAN length window.
MIN_CARD_DIGITS = 13
MAX_CARD_DIGITS = 19


def non_card_reason(match: str) -> str:
    """Return why a CREDIT_CARD match is noise, or "" when it could be a real PAN.

    This layer sees only the matched value. The noise class it cannot reach — a
    perfectly plausible PAN that is really an order id or a hash fragment — is
    handled by ``card_context_reason`` instead.

    Why the value-only layer matters: Presidio's ``CreditCardRecognizer`` matches
    ``\\b(?!1\\d{12}(?!\\d))((4\\d{3})|(5[0-5]\\d{2})|(6\\d{3})|(1\\d{3})|(3\\d{3}))``
    followed by ``[- ]?(\\d{3,4})[- ]?(\\d{3,4})[- ]?(\\d{3,5})\\b``
    and, when the Luhn checksum passes, ``PatternRecognizer`` raises the score to
    1.0 with no context requirement. Roughly one in ten 16-digit runs beginning
    with 1, 3, 4, 5 or 6 clears that bar, so consecutive PR numbers printed by
    ``gh``, concatenated ids, and any other digit soup in tool output is reported
    as cardholder data at maximum confidence (AIS-720).

    Four checks, in order:

      1. Shape. Anything outside the 13-19 digit window (after the separators the
         recognizer's own grammar allows are stripped) is left alone: it did not
         come from this recognizer, so this catalog has nothing to say about it.
      2. Luhn. The recognizer only reports checksum-valid runs, so a failing value
         never was a card. Reimplemented here so the offline sweep can re-judge a
         stored finding without calling the analyzer.
      3. Published test PANs. Payment processors publish sandbox card numbers that
         can never be charged, and they are everywhere in fixtures, seed data and
         docs. A coding agent's PostToolUse hook ships the whole original file with
         every edit, so one fixture re-reports the same PANs indefinitely.
      4. Placeholder shapes and unissued prefixes. A run built from two distinct
         digits, or whose four-digit groups count up one by one, is a pattern
         rather than an account; and a PAN always begins inside an issuer
         identification range that ISO/IEC 7812 actually allocates, which
         Presidio's leading-group alternation is far wider than.
    """
    digits = _card_digits(match)
    if not MIN_CARD_DIGITS <= len(digits) <= MAX_CARD_DIGITS:
        return ""
    if not luhn_valid(digits):
        return "fails the Luhn checksum"
    if digits in KNOWN_TEST_PANS:
        return "published payment-processor test card number"
    shape = _placeholder_card_shape_reason(digits)
    if shape:
        return shape
    if not issued_card_prefix(digits):
        return "does not begin with an issued card-network prefix"
    return ""


def card_context_reason(text: str) -> str:
    """Report a CREDIT_CARD match as noise when its text never mentions payment cards.

    The recognizer ships CONTEXT words ("credit", "card", "visa", ...) but
    Presidio only uses them to *raise* a score, never to gate a match, and a
    passing Luhn check pins the score at 1.0 before any context is consulted.
    This inverts that: a card-shaped digit run in a payload that never talks
    about payments is treated as an opaque identifier rather than cardholder
    data.

    The trade-off is deliberate and matches the UK NHS catalog. A real PAN pasted
    with no payment vocabulary anywhere in the payload is missed; in exchange,
    coding-agent traffic — where card-shaped runs are overwhelmingly ids, offsets
    and CLI output — stops producing warn challenges on a financial policy. The
    vocabulary below is deliberately generous because the value-only layer
    already removes the two dominant noise families, and keeping a finding is the
    cheap direction.

    ``text`` is the whole scanned payload, not a window around the match, so that
    this agrees with the offline sweep. An empty ``text`` means "context
    unknown", and no finding is suppressed on that basis.
    """
    if not text:
        return ""
    lower = text.lower()
    if any(_contains_word(lower, word) for word in CARD_CONTEXT_WORDS):
        return ""
    if any(term in lower for term in CARD_CONTEXT_TERMS):
        return ""
    return "card-shaped digit run with no payment-card context in the surrounding text"


def _contains_word(lower: str, term: str) -> bool:
    """Report whether ``term`` occurs in ``lower`` as a standalone token, bounded
    on both sides by something that is not an ASCII letter or digit.

    This is what makes the recognizer's own generic context words usable as a
    gate. Presidio matches "credit" and "card" as substrings, which fire inside
    "credited", "discard", "wildcard", "cardinality" and "scorecard" — all
    ordinary vocabulary in the traffic this catalog de-noises. As tokens they
    still match every shape that actually labels a card: ``Card:``, ``"card":``,
    ``card_number``, ``CARD-NUMBER``, ``credit_card``.

    ``lower`` must already be lowercased; anything outside ``[a-z0-9]`` counts as
    a boundary.
    """
    start = lower.find(term)
    while start >= 0:
        end = start + len(term)
        if not _is_word_char(lower, start - 1) and not _is_word_char(lower, end):
            return True
        start = lower.find(term, start + 1)
    return False


def _is_word_char(s: str, i: int) -> bool:
    if i < 0 or i >= len(s):
        return False
    ch = s[i]
    return ("a" <= ch <= "z") or ("0" <= ch <= "9")


def _card_digits(match: str) -> str:
    """Strip the separators the credit-card recognizer's grammar allows (spaces
    and hyphens, per its ``replacement_pairs``) and return the remaining
    characters only when every one of them is a digit. Anything else returns ""
    so callers treat the value as out of scope rather than mis-measuring it.
    """
    out: list[str] = []
    for ch in match.strip():
        if ch in " -":
            continue
        if not ch.isascii() or not ch.isdigit():
            return ""
        out.append(ch)
    return "".join(out)


def luhn_valid(digits: str) -> bool:
    """Run the mod-10 check every payment card carries: double every second digit
    from the right, cast out nines, and require the total to be divisible by ten.
    Same validation as Presidio's ``CreditCardRecognizer``.
    """
    if not digits:
        return False
    total = 0
    double = False
    for ch in reversed(digits):
        d = int(ch)
        if double:
            d *= 2
            if d > 9:
                d -= 9
        total += d
        double = not double
    return total % 10 == 0


def _placeholder_card_shape_reason(digits: str) -> str:
    """Report the digit-pattern families that are card-shaped by construction
    rather than by being an account number. Each is something a person or a
    program typed as a series, so a passing Luhn check is coincidence.
    """
    # Two or fewer distinct digits is the 4111-1111-1111-1111 /
    # 4242-4242-4242-4242 family: a random 16-digit number has a ~1-in-10^10
    # chance of being that repetitive, so this never costs a real card.
    if len(set(digits)) <= 2:
        return "built from at most two distinct digits"
    if _consecutive_groups(digits):
        return "a run of consecutive four-digit numbers, not one card"
    if _consecutive_run(digits):
        return "an unbroken ascending or descending digit run"
    return ""


def _consecutive_groups(digits: str) -> bool:
    """Report whether the run splits into four-digit groups that step by exactly
    one, in either direction — the shape a ``gh pr list`` (or any other listing of
    adjacent ids) takes when four of them land side by side. Requires at least
    three groups so an 8-digit coincidence cannot trip it.
    """
    group = 4
    if len(digits) % group != 0 or len(digits) // group < 3:
        return False
    values = [int(digits[i : i + group]) for i in range(0, len(digits), group)]
    step = values[1] - values[0]
    if step not in (1, -1):
        return False
    return all(b - a == step for a, b in itertools.pairwise(values))


def _consecutive_run(digits: str) -> bool:
    """Report whether every digit but the last steps by one (mod ten) from the
    digit before it — 1234-5678-9012-345X and its descending twin. The final digit
    is excluded because a Luhn check digit is whatever the checksum demands, so it
    breaks the run in all but one case in ten.
    """
    if len(digits) < 5:
        return False
    body = digits[:-1]
    step = (int(body[1]) - int(body[0])) % 10
    if step not in (1, 9):  # 9 is -1 mod 10.
        return False
    return all((int(b) - int(a)) % 10 == step for a, b in itertools.pairwise(body))


def issued_card_prefix(digits: str) -> bool:
    """Report whether the run begins inside one of the allocated IIN ranges."""
    for low, high in ISSUED_CARD_PREFIXES:
        n = len(low)
        if len(digits) < n:
            continue
        if low <= digits[:n] <= high:
            return True
    return False


# Inclusive ranges over a PAN's leading digits; the two bounds of each entry
# always have the same length, so the comparison is a plain lexicographic one
# over equal-length digit strings.
#
# Presidio's leading-group alternation accepts any run starting 1, 3, 4, 5(0-5)
# or 6, which is far wider than the allocated space — 66xx, for example, matches
# the recognizer but belongs to no network, so a Luhn-valid run there was never a
# card.
#
# Sourced from ISO/IEC 7812 major industry identifiers and each network's
# published BIN ranges (Visa, Mastercard, American Express, Discover, Diners
# Club, JCB, UnionPay, Maestro, Mir, RuPay, Elo/Hipercard). Ranges are kept at
# the granularity the networks publish rather than narrowed to the BINs seen in
# practice: an over-narrow table would silently drop real cards, which is the
# expensive direction for this catalog.
#
# Some entries (the 2-series and 8-series) cannot currently be produced by the
# recognizer's regex at all. They are listed anyway so the table reads as "what a
# card can start with" rather than "what this Presidio version emits".
ISSUED_CARD_PREFIXES: tuple[tuple[str, str], ...] = (
    ("1", "1"),  # UATP (air travel), 15 digits
    ("2200", "2204"),  # Mir
    ("2221", "2720"),  # Mastercard 2-series
    ("300", "305"),  # Diners Club International
    ("3095", "3095"),  # Diners Club International
    ("34", "34"),  # American Express
    ("3528", "3589"),  # JCB
    ("36", "36"),  # Diners Club International
    ("37", "37"),  # American Express
    ("38", "39"),  # Diners Club (UK/Ireland, enRoute)
    ("4", "4"),  # Visa
    ("50", "50"),  # Maestro
    ("51", "55"),  # Mastercard
    ("56", "58"),  # Maestro
    ("60", "60"),  # Discover (6011), RuPay
    ("62", "62"),  # UnionPay, Discover co-brands
    ("6304", "6304"),  # Maestro
    ("636", "636"),  # Elo
    ("637", "639"),  # InstaPayment
    ("64", "65"),  # Discover
    ("67", "67"),  # Maestro, Laser
    ("81", "82"),  # UnionPay, RuPay
)

# Lowercased tokens that count as payment-card signal, matched with
# ``_contains_word`` so each must appear as a whole word. The list starts from the
# recognizer's own CONTEXT words — kept generic precisely because token matching
# makes them safe — and adds the networks, processors and acquiring vocabulary
# that name cardholder data without ever saying "card".
CARD_CONTEXT_WORDS: tuple[str, ...] = (
    # The recognizer's own context vocabulary.
    "card",
    "cards",
    "credit",
    "credits",
    "debit",
    "debits",
    "visa",
    "mastercard",
    "amex",
    "discover",
    "jcb",
    "diners",
    "maestro",
    "instapayment",
    # Networks the recognizer omits.
    "unionpay",
    "rupay",
    "interac",
    "elo",
    "hipercard",
    # Field names and card data that travel with a PAN.
    "cardholder",
    "pan",
    "cvv",
    "cvv2",
    "cvc",
    "cvc2",
    "expiry",
    "expiration",
    # Processors and acquiring vocabulary: a payload that talks to one of these is
    # handling cardholder data even when it never says "card".
    "stripe",
    "braintree",
    "adyen",
    "worldpay",
    "chargeback",
    "chargebacks",
    "acquirer",
    "pci",
)

# Substrings for the compound spellings token matching cannot see: a camelCase or
# run-together identifier has no boundary between its parts, so "cardNumber"
# lowercases to a single token that neither "card" nor "number" matches as a word.
CARD_CONTEXT_TERMS: tuple[str, ...] = (
    "creditcard",
    "debitcard",
    "cardnumber",
    "cardnum",
    "cardbrand",
    "cardtype",
    "cardexpiry",
    "paymentcard",
    "paymentmethod",
    "expmonth",
    "expyear",
    "american express",
    "authorize.net",
    "checkout.com",
    "primary account number",
    "issuing bank",
    "merchant account",
    "security code",
)

# Published, non-chargeable sandbox card numbers from the payment networks' and
# processors' own documentation (Visa, Mastercard, American Express, Discover,
# Diners Club, JCB, UnionPay, Maestro, Elo/Hipercard; Stripe, Adyen, Braintree,
# PayPal, Worldpay). Stored as bare digits; ``_card_digits`` strips separators
# before the lookup, so spaced and hyphenated spellings match too.
#
# These dominate the false positives in practice. They live in fixtures, seed
# data, SDK examples and docs, and a coding agent's PostToolUse hook re-ships the
# whole file on every edit, so one fixture re-reports the same PANs indefinitely.
#
# The Stripe 4000-00* family is enumerated rather than collapsed into a BIN
# range: each entry is a documented behaviour trigger, and carving out the whole
# 400000 BIN would suppress a wider slice of Visa space than the documentation
# actually reserves.
#
# ``test_known_test_pans_are_well_formed`` keeps every entry Luhn-valid and in the
# recognizer's length window.
KNOWN_TEST_PANS: frozenset[str] = frozenset(
    {
        # Visa.
        "4111111111111111",
        "4012888888881881",
        "4222222222222",
        "4242424242424242",
        "4000056655665556",
        "4917610000000000",
        "4005519200000004",
        "4009348888881881",
        "4012000033330026",
        "4012000077777777",
        "4217651111111119",
        "4500600000000061",
        "4444333322221111",
        "4462030000000000",
        "4484070000000000",
        "4988438843884305",
        "4977949494949497",
        "4646464646464644",
        "4543474002249996",
        # Visa, Stripe's documented behaviour triggers.
        "4000000000000002",
        "4000000000000010",
        "4000000000000028",
        "4000000000000036",
        "4000000000000044",
        "4000000000000069",
        "4000000000000077",
        "4000000000000093",
        "4000000000000101",
        "4000000000000119",
        "4000000000000127",
        "4000000000000259",
        "4000000000000341",
        "4000000000002685",
        "4000000000003055",
        "4000000000003063",
        "4000000000003220",
        "4000000000005423",
        "4000000000009979",
        "4000000000009987",
        "4000000000009995",
        "4000002500003155",
        "4000002760003184",
        "4000008400001629",
        # Mastercard.
        "5555555555554444",
        "5105105105105100",
        "5200828282828210",
        "5555341244441115",
        "5454545454545454",
        "5431111111111111",
        "5111005111051128",
        "5577000055770004",
        "5555444433331111",
        "5500000000000004",
        "5425233430109903",
        "5100060000000002",
        # Mastercard 2-series.
        "2223003122003222",
        "2223000048400011",
        "2223000048410010",
        "2222400010000008",
        "2222400030000004",
        # American Express.
        "378282246310005",
        "371449635398431",
        "378734493671000",
        "340000000000009",
        "374245455400126",
        "375987000000005",
        "343434343434343",
        # Discover.
        "6011111111111117",
        "6011000990139424",
        "6011000400000000",
        "6011601160116611",
        "6011981111111113",
        "6445644564456445",
        # Diners Club.
        "30569309025904",
        "38520000023237",
        "36006666333344",
        "36148900647913",
        # JCB.
        "3530111333300000",
        "3566002020360505",
        "3569990010030400",
        "3528000700000000",
        # UnionPay.
        "6212345678901232",
        "6250941006528599",
        "6243030000000001",
        # Maestro.
        "6759649826438453",
        "6304000000000000",
        "5641821111166669",
        # Elo / Hipercard.
        "5066991111111118",
        "6362970000457013",
        "6062826786276634",
        "6060704495764400",
    }
)
