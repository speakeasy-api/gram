"""Payment-card false-positive catalog.

Mirror of ``server/internal/risk/presidiofp/creditcard.go``; see
``non_card_reason`` for the rationale.
"""

import itertools

# Bounds of a primary account number (PAN) per ISO/IEC 7812. Presidio's
# ``CreditCardRecognizer`` only emits runs inside this window.
MIN_CARD_DIGITS = 13
MAX_CARD_DIGITS = 19

# How far apart the four groups of an id listing may be. Four sorted ids within
# a hundred of each other is the shape of a ``gh pr list`` or similar CLI output;
# a random card number lands there roughly once in three million draws.
CARD_ID_LIST_MAX_SPAN = 100


def non_card_reason(match: str) -> str:
    """Return why a CREDIT_CARD match is noise, or "" when it could be a real card.

    Presidio's ``CreditCardRecognizer`` matches any 13-17 digit run starting with
    1, 3, 4, 5[0-5] or 6 and, when the Luhn checksum passes, ``PatternRecognizer``
    pins the score at 1.0 without consulting its context words. Roughly one in
    ten card-shaped runs passes Luhn, so published test cards in fixtures and
    lists of adjacent ids in CLI output are reported as cardholder data at
    maximum confidence.

    This layer judges the value alone and only rules out values that cannot be a
    real customer's card, so it never needs the surrounding text:

      1. Published test PANs. Payment networks and processors document sandbox
         card numbers that can never be charged. They live in fixtures, seed data
         and docs, and a coding agent's PostToolUse hook re-ships the whole file
         on every edit, so one fixture re-reports the same numbers indefinitely.
      2. Placeholder shapes: a run built from at most two distinct digits
         (4444 5555 5555 5555), or one unbroken ascending or descending digit
         sequence (1234 5678 9012 3452).
      3. Id listings: four sorted four-digit groups close together, such as four
         PR numbers printed side by side (6651 6652 6653 6654).

    Deliberately NOT filtered: a plausible PAN with no card vocabulary around it.
    Neither scanner knows which surface (user message, tool request, tool
    response) a payload came from, and dropping every PAN in a payload without
    payment words would also drop real card numbers in opaque JSON or logs.
    """
    digits = _card_digits(match)
    if not MIN_CARD_DIGITS <= len(digits) <= MAX_CARD_DIGITS:
        return ""
    if digits in KNOWN_TEST_PANS:
        return "published test card number"
    if len(set(digits)) <= 2:
        return "placeholder card number built from at most two distinct digits"
    if _sequential_digits(digits):
        return "placeholder card number made of sequential digits"
    if _card_id_listing(digits):
        return "list of nearby four-digit ids, not a card number"
    return ""


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


def _sequential_digits(digits: str) -> bool:
    """Report whether every digit but the last steps by one (mod ten, in a single
    direction) from the digit before it. The last digit is ignored because a Luhn
    check digit is whatever the checksum demands.
    """
    body = digits[:-1]
    step = (int(body[1]) - int(body[0])) % 10
    if step not in (1, 9):
        return False
    return all((int(b) - int(a)) % 10 == step for a, b in itertools.pairwise(body))


def _card_id_listing(digits: str) -> bool:
    """Report whether a 16-digit run splits into four strictly ascending or
    strictly descending four-digit groups that all fall within
    ``CARD_ID_LIST_MAX_SPAN`` of each other.
    """
    if len(digits) != 16:
        return False
    groups = [int(digits[i : i + 4]) for i in range(0, 16, 4)]
    pairs = list(itertools.pairwise(groups))
    ascending = all(b > a for a, b in pairs)
    descending = all(b < a for a, b in pairs)
    if not ascending and not descending:
        return False
    return abs(groups[3] - groups[0]) <= CARD_ID_LIST_MAX_SPAN


# Published, non-chargeable sandbox card numbers from the payment networks' and
# processors' documentation (Visa, Mastercard, American Express, Discover,
# Diners Club, JCB, UnionPay, Maestro; Stripe, Adyen, Braintree, PayPal,
# Worldpay), plus a few example numbers that circulate widely in docs and
# fake-data generators. Stored as bare digits; ``_card_digits`` strips separators
# before the lookup.
#
# Keep in sync with ``knownTestPANs`` in
# server/internal/risk/presidiofp/creditcard.go.
KNOWN_TEST_PANS: frozenset[str] = frozenset(
    {
        # Visa.
        "4111111111111111",
        "4242424242424242",
        "4012888888881881",
        "4222222222222",
        "4000056655665556",
        "4005519200000004",
        "4009348888881881",
        "4012000033330026",
        "4012000077777777",
        "4217651111111119",
        "4444333322221111",
        "4462030000000000",
        "4484070000000000",
        "4500600000000061",
        "4543474002249996",
        "4646464646464644",
        "4917300800000000",
        "4917610000000000",
        "4988438843884305",
        "4539148803436467",
        # Visa, Stripe's documented decline and authentication triggers.
        "4000000000000002",
        "4000000000000069",
        "4000000000000077",
        "4000000000000119",
        "4000000000000127",
        "4000000000000341",
        "4000000000003055",
        "4000000000003063",
        "4000000000003220",
        "4000000000009979",
        "4000000000009987",
        "4000000000009995",
        "4000002500001001",
        "4000002500003155",
        "4000002760003184",
        "4000008400001629",
        # Mastercard.
        "5555555555554444",
        "5200828282828210",
        "5105105105105100",
        "5454545454545454",
        "5431111111111111",
        "5500000000000004",
        "5555341244441115",
        "5555444433331111",
        "5577000055770004",
        "2223003122003222",
        "2222400010000008",
        "2223000048400011",
        # American Express.
        "378282246310005",
        "371449635398431",
        "378734493671000",
        "340000000000009",
        "370000000000002",
        "374245455400126",
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
        "36227206271667",
        "3056930009020004",
        # JCB.
        "3530111333300000",
        "3566002020360505",
        "3566111111111113",
        "3569990010030400",
        "3528000700000000",
        # UnionPay.
        "6200000000000005",
        "6205500000000000004",
        "6243030000000001",
        "6250941006528599",
        # Maestro.
        "6759649826438453",
        "6304000000000000",
    }
)
