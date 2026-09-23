"""Tests for the presidiofp false-positive classifier.

Mirrors the Go unit tests in
``server/internal/risk/presidiofp/classify_test.go``
(``TestNonPIIIPExactKeysAreCanonical``, ``TestReason``, ``TestReasonByRuleID``),
``nhs_test.go`` and ``retired_test.go``, and adds broader table coverage of the
IP and email catalogs.

(``fp_split_test.go`` is build-tagged dev tooling that regenerates testdata
rather than a unit test, so it has no counterpart here.)
"""

import ipaddress
import random

import pytest

from pystreams.risk import presidiofp
from pystreams.risk.presidiofp import ip_asn
from pystreams.risk.presidiofp.classify import (
    ENTITY_TYPE_CREDIT_CARD,
    ENTITY_TYPE_EMAIL_ADDRESS,
    ENTITY_TYPE_IP_ADDRESS,
    ENTITY_TYPE_UK_NHS,
    _entity_type_for_rule_id,
)
from pystreams.risk.presidiofp.creditcard import (
    ISSUED_CARD_PREFIXES,
    KNOWN_TEST_PANS,
    MAX_CARD_DIGITS,
    MIN_CARD_DIGITS,
    _card_digits,
    _contains_word,
    _placeholder_card_shape_reason,
    card_context_reason,
    issued_card_prefix,
    luhn_valid,
    non_card_reason,
)
from pystreams.risk.presidiofp.ip import _NON_PII_IP_EXACT
from pystreams.risk.presidiofp.nhs import (
    NHS_ALLOCATED_RANGES,
    _nhs_check_digit_valid,
    nhs_context_reason,
    non_nhs_reason,
)
from pystreams.risk.presidiofp.retired import RETIRED_RECOGNIZERS

# A Luhn-valid, correctly-prefixed PAN that is not published in any processor's
# test-card documentation, so it stands in for "a real card" below. Used only as
# the value that MUST survive the catalog.
SYNTHETIC_VISA = "4539172846305125"


def test_non_pii_ip_exact_keys_are_canonical():
    """Mirror of ``TestNonPIIIPExactKeysAreCanonical``.

    Every exact key must already be in canonical ``ipaddress`` form. The lookup
    keys off ``str(addr)``, so a non-canonical key would silently never match.
    """
    for key in _NON_PII_IP_EXACT:
        addr = ipaddress.ip_address(key)  # must parse as an IP
        assert key == str(addr), f"key {key!r} must be in canonical form"


def test_reason():
    """Mirror of ``TestReason``: the entity-keyed dispatch.

    Reserved/placeholder matches return a reason, real ones return "", and only
    the two catalogued entity types fire.
    """
    assert presidiofp.reason(ENTITY_TYPE_IP_ADDRESS, "10.0.0.1"), "RFC1918 IP"
    assert presidiofp.reason(ENTITY_TYPE_IP_ADDRESS, "  ::  "), "trimmed unspecified IP"
    assert presidiofp.reason(ENTITY_TYPE_EMAIL_ADDRESS, "noreply@example.com"), (
        "placeholder email"
    )
    assert presidiofp.reason(ENTITY_TYPE_CREDIT_CARD, "4111 1111 1111 1111"), (
        "published test card"
    )

    assert not presidiofp.reason(ENTITY_TYPE_IP_ADDRESS, "71.126.87.167"), (
        "residential IP"
    )
    assert not presidiofp.reason(ENTITY_TYPE_EMAIL_ADDRESS, "ada@speakeasy.com"), (
        "real email"
    )
    assert not presidiofp.reason(ENTITY_TYPE_CREDIT_CARD, SYNTHETIC_VISA), (
        "real-shaped card, no context supplied"
    )

    # Uncatalogued entity types never fire, even on a value another lane would flag.
    assert presidiofp.reason("PERSON", "10.0.0.1") == ""
    assert presidiofp.reason("", "10.0.0.1") == ""


def test_reason_by_rule_id():
    """Mirror of ``TestReasonByRuleID``: the rule_id-keyed entry point used to
    re-evaluate stored findings, plus the rule_id<->entity grammar.
    """
    assert presidiofp.reason_by_rule_id("pii.ip_address", "10.0.0.1"), "RFC1918 IP"
    assert presidiofp.reason_by_rule_id("pii.email_address", "noreply@example.com"), (
        "placeholder email"
    )

    assert not presidiofp.reason_by_rule_id("pii.ip_address", "71.126.87.167"), (
        "residential IP"
    )

    # Rule ids without a catalog never fire, even when the match would match
    # another lane's catalog.
    assert presidiofp.reason_by_rule_id("pii.person", "10.0.0.1") == ""
    assert presidiofp.reason_by_rule_id("secret.aws_access_key", "10.0.0.1") == ""
    assert presidiofp.reason_by_rule_id("", "10.0.0.1") == ""

    # rule_ids advertises exactly the catalogued rule ids, and the grammar is
    # invertible.
    assert presidiofp.rule_ids() == [
        "pii.ip_address",
        "pii.email_address",
        "pii.uk_nhs",
        "pii.credit_card",
        "pii.us_driver_license",
    ]
    assert presidiofp.context_rule_ids() == ["pii.uk_nhs", "pii.credit_card"]
    assert _entity_type_for_rule_id("pii.ip_address") == "IP_ADDRESS"
    assert _entity_type_for_rule_id("pii.email_address") == "EMAIL_ADDRESS"
    assert _entity_type_for_rule_id("secret.aws_access_key") == ""


@pytest.mark.parametrize(
    ("match", "expect_fp"),
    [
        # Reserved / private / special address space.
        ("10.0.0.1", True),
        ("172.16.5.4", True),
        ("192.168.1.1", True),
        ("127.0.0.1", True),
        ("::1", True),
        ("169.254.1.1", True),
        ("224.0.0.1", True),
        ("100.64.0.1", True),
        ("192.0.2.5", True),  # documentation range
        ("255.255.255.255", True),
        # Well-known public resolvers (exact catalog).
        ("1.1.1.1", True),
        ("8.8.8.8", True),
        ("9.9.9.9", True),
        # Equivalent non-canonical spelling of a catalogued resolver still resolves.
        ("2606:4700:4700:0:0:0:0:1111", True),
        # Common placeholder literals.
        ("1.2.3.4", True),
        ("2.2.2.2", True),
        # Shape heuristics.
        ("73.0.0.0", True),  # network address of a public /8
        ("dead::", True),  # sparse IPv6
        # IPv6 unique-local space (RFC 4193), including a "dense" ULA the sparse
        # heuristic would miss.
        ("fc00::1", True),
        ("fd12:3456:789a:1::1", True),
        # Cloud / CDN / hosting via ASN lookup.
        ("52.94.236.248", True),  # Amazon AS16509
        # Unparseable input is never a false positive.
        ("not-an-ip", False),
    ],
)
def test_ip_reason(match: str, expect_fp: bool):
    reason = presidiofp.reason("IP_ADDRESS", match)
    assert bool(reason) is expect_fp


@pytest.mark.parametrize(
    "org",
    ["Comcast Cable Communications, LLC", "Verizon Business", "AT&T Services, Inc."],
)
def test_consumer_isp_ip_passes_through_as_pii(monkeypatch, org: str):
    """A real consumer-ISP address is PII and must pass through, not be suppressed.

    Consumer-ISP brands are deliberately excluded from the infra ASN regex (see
    ``_INFRA_ASN_RE``). Drive that branch with a stubbed reader rather than
    committing a real customer's IP as fixture data: 198.51.100.7 is a TEST-NET-2
    documentation address (RFC 5737) that reaches the ASN fall-through.
    """

    class _StubReader:
        def get(self, addr: str) -> dict[str, object]:
            return {
                "autonomous_system_number": 7922,
                "autonomous_system_organization": org,
            }

    monkeypatch.setattr(ip_asn, "_load_reader", lambda: _StubReader())
    # The address itself is irrelevant — the stub answers for any input — so use a
    # documentation address that carries no real-world attribution.
    assert ip_asn.infra_asn_reason("198.51.100.7") == ""


@pytest.mark.parametrize(
    ("match", "expect_fp"),
    [
        ("user@example.com", True),  # placeholder domain
        ("svc@acme.io", True),  # placeholder SLD + TLD
        ("a@host.test", True),  # RFC 6761 reserved TLD
        ("1f615@2x.png", True),  # image-extension "TLD"
        ("medium.com/@user", True),  # contains '/'
        ("pkg@v1.2.3", True),  # version suffix (trailing digit)
        ("noreply@realcorp.com", True),  # automated local-part
        ("first.last@realcorp.com", True),  # template local-part
        ("git@github.com", True),  # known false positive
        # Real-looking addresses that must pass through.
        ("jane@realcorp.com", False),
        ("a@b.com", False),
        ("john.doe@acmebank.co", False),  # placeholder TLD set excludes .co
        ("john.doe@example.com", True),  # but a placeholder domain still fires
    ],
)
def test_email_reason(match: str, expect_fp: bool):
    reason = presidiofp.reason("EMAIL_ADDRESS", match)
    assert bool(reason) is expect_fp


def test_email_trailing_digit_is_ascii_only():
    """A trailing ASCII digit reads as a version suffix; a trailing Unicode digit
    does not (matches the Go ``'0'..'9'`` bound, not Python's ``str.isdigit()``).
    """
    assert presidiofp.reason("EMAIL_ADDRESS", "pkg@v1") != ""
    # U+00B2 SUPERSCRIPT TWO is a Unicode digit but not ASCII; must not fire.
    assert presidiofp.reason("EMAIL_ADDRESS", "user@example²") == ""


@pytest.mark.parametrize(
    ("match", "expect_fp"),
    [
        # Issued ranges, valid check digit: this layer must let them through.
        ("401 023 2137", False),
        ("401-023-2137", False),
        ("4010232137", False),
        ("6543210982", False),  # Wales range
        ("1706349017", False),  # Scotland CHI range
        ("3201234567", False),  # Northern Ireland range
        # Never issued to anyone.
        ("9999999999", True),  # NHS England test range
        ("9434765919", True),  # unallocated above 859
        ("0000000000", True),  # zero-padded internal id
        # Not an NHS number at all.
        ("4010232138", True),  # check digit fails
        # Out of this catalog's scope: the recognizer only ever emits ten-digit
        # runs, so anything else is left for another lane.
        ("40102321", False),
        ("40102321370", False),
        ("40102321AB", False),
        ("", False),
    ],
)
def test_non_nhs_reason(match: str, expect_fp: bool):
    """Mirror of ``TestNonNHSReason``: the value-only layer, i.e. what a ten-digit
    run says about itself before any surrounding text is consulted.
    """
    assert bool(non_nhs_reason(match)) is expect_fp


@pytest.mark.parametrize(
    "text",
    [
        "Patient NHS number 401 023 2137",
        '{"nhsNumber": "4010232137"}',
        "NHS_NUMBER=4010232137",
        "the national health service record shows 4010232137",
        "CHI number 1706349017 for the Scottish record",
        "hospital number on file, id 4010232137",
        # Unknown context is not evidence of anything.
        "",
    ],
)
def test_nhs_context_reason_keeps(text: str):
    """Mirror of ``TestNHSContextReason``'s kept half."""
    assert nhs_context_reason(text) == ""


@pytest.mark.parametrize(
    "text",
    [
        "https://acme.atlassian.net/wiki/spaces/ENG/pages/4010232137/Runbook",
        '{"ts": 1706349017, "level": "info"}',
        "order 4010232137 shipped",
        "figma node 4010232137",
    ],
)
def test_nhs_context_reason_suppresses(text: str):
    """Mirror of ``TestNHSContextReason``'s suppressed half."""
    assert nhs_context_reason(text) != ""


def test_nhs_check_digit_matches_presidio():
    """Mirror of ``TestNHSCheckDigitMatchesPresidio``: lock the reimplemented
    mod-11 check to the one Presidio's ``NhsRecognizer`` runs.
    """
    valid = ("4010232137", "9434765919", "1706349017", "2481160193", "9999999999")
    for digits in valid:
        assert _nhs_check_digit_valid(digits), f"{digits} should pass"
    invalid = ("4010232138", "0001234567", "6543210989", "1234567890", "3201234561")
    for digits in invalid:
        assert not _nhs_check_digit_valid(digits), f"{digits} should fail"


def test_nhs_allocated_ranges_are_ordered_and_disjoint():
    """Mirror of ``TestNHSAllocatedRangesAreOrderedAndDisjoint``: a low above its
    high, or a pair of overlapping ranges, means someone mistyped a boundary.
    """
    for i, (low, high) in enumerate(NHS_ALLOCATED_RANGES):
        assert low <= high, f"range {i} is inverted"
        if i == 0:
            continue
        assert low > NHS_ALLOCATED_RANGES[i - 1][1], (
            f"range {i} overlaps or backtracks on its predecessor"
        )


def test_nhs_suppresses_opaque_identifiers():
    """Mirror of ``TestNHSSuppressesOpaqueIdentifiers``, the regression this
    catalog exists for (AIS-494). Presidio reports any checksum-valid ten-digit
    run as a UK NHS number at maximum confidence, so roughly one in eleven
    Confluence page ids, Unix timestamps and order numbers surfaces as a
    government/health identifier. Every one must now be classified as noise.
    """
    # Deterministic corpus, seeded so a failure is reproducible.
    rng = random.Random(1)

    checked = 0
    for _ in range(20000):
        identifier = f"{rng.randrange(1_000_000_000, 10_000_000_000):010d}"
        if not _nhs_check_digit_valid(identifier):
            continue  # Presidio would not have reported it in the first place.
        checked += 1
        text = f"https://acme.atlassian.net/wiki/spaces/ENG/pages/{identifier}/Runbook"
        assert presidiofp.reason_in_context(ENTITY_TYPE_UK_NHS, identifier, text), (
            f"opaque identifier {identifier} must not read as an NHS number"
        )
    assert checked > 0, "corpus produced no checksum-valid ids"


@pytest.mark.parametrize(
    ("match", "expect_fp"),
    [
        # Real-shaped PANs: this layer must let them through.
        (SYNTHETIC_VISA, False),
        ("4539 1728 4630 5125", False),
        ("4539-1728-4630-5125", False),
        ("5534129876004319", False),
        ("371882450931763", False),
        # Published sandbox PANs from processor documentation.
        ("4111111111111111", True),
        ("4111 1111 1111 1111", True),
        ("4242424242424242", True),
        ("5555555555554444", True),
        ("378282246310005", True),
        ("6011111111111117", True),
        ("30569309025904", True),
        ("3530111333300000", True),
        # Placeholder shapes.
        ("4141414141414141", True),  # two distinct digits
        ("4009401040114012", True),  # consecutive four-digit groups
        ("1234567890123452", True),  # consecutive digit run
        # Prefixes no network issues from.
        ("6651665266536654", True),
        ("6135802974216083", True),
        # Presidio only reports checksum-valid runs, so a failing one is noise
        # by construction (and the offline sweep re-checks stored values).
        ("4539172846305126", True),
        # Out of this catalog's scope: the recognizer never emits these shapes.
        ("453917284630", False),
        ("45391728463051250000", False),
        ("4539-1728-4630-51AB", False),
        ("", False),
    ],
)
def test_non_card_reason(match: str, expect_fp: bool):
    """Mirror of ``TestNonCardReason``: the value-only layer, i.e. what a
    card-shaped digit run says about itself before any surrounding text is read.
    """
    assert bool(non_card_reason(match)) is expect_fp


@pytest.mark.parametrize(
    "text",
    [
        f"Customer credit card {SYNTHETIC_VISA}",
        f"Card: {SYNTHETIC_VISA}",
        f'{{"card": {{"number": "{SYNTHETIC_VISA}"}}}}',
        f'{{"card_number": "{SYNTHETIC_VISA}"}}',
        f'{{"cardNumber":"{SYNTHETIC_VISA}","cvv":"123"}}',
        f"CARD_NUMBER={SYNTHETIC_VISA}",
        "charge the Visa ending 5125",
        f"stripe.paymentMethods.create({{ number: '{SYNTHETIC_VISA}' }})",
        "cardholder data must never be logged",
        "PCI DSS scope review",
        # Unknown context is not evidence of anything.
        "",
    ],
)
def test_card_context_reason_keeps(text: str):
    """Mirror of ``TestCardContextReason``'s kept half."""
    assert card_context_reason(text) == ""


@pytest.mark.parametrize(
    "text",
    [
        "gh pr view 6651 6652 6653 6654",
        f'{{"trace_id": "{SYNTHETIC_VISA}", "level": "info"}}',
        f"order {SYNTHETIC_VISA} shipped",
        # The words Presidio's own CONTEXT list would have matched, in the
        # ordinary code senses that make substring matching unusable as a gate.
        "discard the wildcard entry and recompute cardinality",
        "the account was credited last night",
        "scorecard rendered by DiscoveryPanel",
    ],
)
def test_card_context_reason_suppresses(text: str):
    """Mirror of ``TestCardContextReason``'s suppressed half."""
    assert card_context_reason(text) != ""


def test_contains_word():
    """Mirror of ``TestContainsWord``: the generic words the recognizer ships are
    only safe as a gate because they are matched as whole tokens, with
    punctuation and separators counting as boundaries.
    """
    for text in (
        "card",
        "card: 1",
        '{"card":1}',
        "card_number",
        "card-number",
        "a card here",
        "CARD",
    ):
        assert _contains_word(text.lower(), "card"), f"should match: {text!r}"
    for text in (
        "discard",
        "wildcard",
        "cardinality",
        "scorecard",
        "cards2",
        "",
        "car",
    ):
        assert not _contains_word(text.lower(), "card"), f"should not match: {text!r}"


def test_credit_card_regressions():
    """Mirror of ``TestCreditCardRegressions`` (AIS-720): the exact traffic shapes
    a financial-data policy was warning on.
    """
    # A fixture-bearing file re-shipped by a coding agent's PostToolUse hook. It
    # talks about cards all over, so only the test-PAN layer can clear it.
    seed_file = (
        "INSERT INTO risk_results (match, rule_id) VALUES\n"
        "('4111 1111 1111 1111', 'pii.credit_card'),\n"
        "('4242424242424242', 'pii.credit_card');"
    )
    for pan in ("4111 1111 1111 1111", "4242424242424242"):
        assert presidiofp.reason_in_context(ENTITY_TYPE_CREDIT_CARD, pan, seed_file), (
            f"test fixture PAN {pan!r} must not flag"
        )

    # A Bash tool request listing four consecutive PR numbers.
    assert presidiofp.reason_in_context(
        ENTITY_TYPE_CREDIT_CARD,
        "6651 6652 6653 6654",
        "gh pr view 6651 6652 6653 6654 --json title",
    ), "consecutive PR numbers must not flag"

    # A genuine-looking card in a payload that names it still flags.
    assert (
        presidiofp.reason_in_context(
            ENTITY_TYPE_CREDIT_CARD,
            SYNTHETIC_VISA,
            f"Customer's credit card on file is {SYNTHETIC_VISA}",
        )
        == ""
    )
    assert (
        presidiofp.reason_in_context(
            ENTITY_TYPE_CREDIT_CARD,
            SYNTHETIC_VISA,
            f'{{"payment_method": {{"card": {{"number": "{SYNTHETIC_VISA}"}}}}}}',
        )
        == ""
    )


def test_credit_card_suppresses_opaque_identifiers():
    """Mirror of ``TestCreditCardSuppressesOpaqueIdentifiers``: about one in ten
    card-shaped digit runs passes Luhn, and Presidio reports every one of those at
    maximum confidence. None may survive the catalog.
    """
    rng = random.Random(720)

    checked = 0
    for _ in range(20000):
        # A 16-digit run whose leading group Presidio's regex accepts.
        identifier = f"{rng.randrange(4000, 7000)}{rng.randrange(10**12):012d}"
        if not luhn_valid(identifier):
            continue  # Presidio would not have reported it in the first place.
        checked += 1
        text = f"build artifact sha stream offset {identifier} written to disk"
        assert presidiofp.reason_in_context(
            ENTITY_TYPE_CREDIT_CARD, identifier, text
        ), f"opaque identifier {identifier} must not read as a card number"
    assert checked > 0, "corpus produced no checksum-valid runs"


def test_known_test_pans_are_well_formed():
    """Mirror of ``TestKnownTestPANsAreWellFormed``: every entry must be bare
    digits, Luhn-valid, inside the recognizer's length window, and carry an issued
    network prefix — a mistyped digit would otherwise match nothing.
    """
    assert KNOWN_TEST_PANS
    for pan in KNOWN_TEST_PANS:
        assert pan == _card_digits(pan), f"{pan} must be stored as bare digits"
        assert MIN_CARD_DIGITS <= len(pan) <= MAX_CARD_DIGITS, f"{pan} is out of window"
        assert luhn_valid(pan), f"{pan} must pass the Luhn checksum"
        assert issued_card_prefix(pan), f"{pan} must start with an issued prefix"


def test_issued_card_prefixes_are_well_formed():
    """Mirror of ``TestIssuedCardPrefixesAreWellFormed``: bounds of differing
    length, or a low above its high, means someone mistyped a boundary.
    """
    for i, (low, high) in enumerate(ISSUED_CARD_PREFIXES):
        assert low, f"range {i} is empty"
        assert len(low) == len(high), f"range {i} has mismatched bound lengths"
        assert low <= high, f"range {i} is inverted"
        assert low == _card_digits(low), f"range {i} low must be digits"
        assert high == _card_digits(high), f"range {i} high must be digits"


@pytest.mark.parametrize(
    ("network", "pan"),
    [
        ("visa", "4539172846305125"),
        ("mastercard", "5534129876004319"),
        ("mastercard 2-series", "2223003122003222"),
        ("amex 34", "340000000000009"),
        ("amex 37", "371449635398431"),
        ("discover 6011", "6011111111111117"),
        ("discover 65", "6500000000000002"),
        ("diners 305", "30569309025904"),
        ("diners 36", "36006666333344"),
        ("jcb", "3530111333300000"),
        ("unionpay", "6212345678901232"),
        ("maestro 67", "6759649826438453"),
        ("uatp", "135412345678911"),
    ],
)
def test_issued_card_prefix_covers_the_networks(network: str, pan: str):
    """Mirror of ``TestIssuedCardPrefixCoversTheNetworks``: narrowing a range in
    future fails here rather than silently suppressing a live card.
    """
    assert issued_card_prefix(pan), f"{network} ({pan}) must be recognized as issued"


@pytest.mark.parametrize(
    "pan", ["6651665266536654", "6135802974216083", "6900000000000008"]
)
def test_unissued_card_prefixes(pan: str):
    """Ranges the networks do not issue from, which Presidio nonetheless matches."""
    assert not issued_card_prefix(pan)


def test_luhn_valid_matches_presidio():
    """Mirror of ``TestLuhnValidMatchesPresidio``."""
    for digits in (
        "4111111111111111",
        "378282246310005",
        "30569309025904",
        SYNTHETIC_VISA,
    ):
        assert luhn_valid(digits), f"{digits} should pass"
    for digits in (
        "4111111111111112",
        "378282246310006",
        "30569309025905",
        "",
        "1234567890123456",
    ):
        assert not luhn_valid(digits), f"{digits} should fail"


def test_placeholder_card_shapes():
    """Mirror of ``TestPlaceholderCardShapes``."""
    for digits in (
        "4111111111111111",  # two distinct digits
        "4242424242424242",  # two distinct digits
        "4009401040114012",  # groups counting up
        "4048404740464045",  # groups counting down
        "1234567890123452",  # one ascending run, wrapping 9->0
    ):
        assert _placeholder_card_shape_reason(digits), (
            f"{digits} should read as a pattern"
        )

    for digits in (
        SYNTHETIC_VISA,
        "5534129876004319",
        "378282246310005",
        "4009401040114013",  # last group breaks the run
    ):
        assert not _placeholder_card_shape_reason(digits), f"{digits} is not a pattern"


def test_card_separators_match_recognizer_grammar():
    """Mirror of ``TestCreditCardSeparatorsMatchRecognizerGrammar``: spaces and
    hyphens only, per the recognizer's ``replacement_pairs``.
    """
    assert _card_digits("4111 1111 1111 1111") == "4111111111111111"
    assert _card_digits("4111-1111-1111-1111") == "4111111111111111"
    assert _card_digits("  4111 1111-1111 1111  ") == "4111111111111111"
    assert _card_digits("4111.1111.1111.1111") == ""
    assert _card_digits("4111_1111_1111_1111") == ""
    assert _card_digits("not a card") == ""


def test_retired_recognizers():
    """Mirror of ``TestRetiredRecognizers``: every finding from a retired
    recognizer is noise regardless of its value, which is what lets the offline
    sweep clear the rows stored before the live scanners started dropping them.
    """
    # The shapes AIS-494 reported: Figma file and node ids read as a driver
    # license number to the upstream recognizer.
    for match in ("N1234567", "K9182736450", "X12345678", ""):
        assert presidiofp.reason("US_DRIVER_LICENSE", match), (
            f"every US_DRIVER_LICENSE finding is retired noise, including {match!r}"
        )

    # Retirement is keyed on the entity, not the value: the same string under a
    # live recognizer is judged on its merits.
    assert presidiofp.reason("US_DRIVER_LICENSE_OTHER", "N1234567") == ""
    assert list(RETIRED_RECOGNIZERS) == ["US_DRIVER_LICENSE"]

    # Context cannot rescue it either, and the rule_id entry point agrees.
    assert presidiofp.reason_in_context(
        "US_DRIVER_LICENSE", "D1234567", "driver license D1234567"
    )
    assert presidiofp.reason_by_rule_id("pii.us_driver_license", "D1234567")
