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
    ENTITY_TYPE_EMAIL_ADDRESS,
    ENTITY_TYPE_IP_ADDRESS,
    ENTITY_TYPE_UK_NHS,
    _entity_type_for_rule_id,
)
from pystreams.risk.presidiofp.ip import _NON_PII_IP_EXACT
from pystreams.risk.presidiofp.nhs import (
    NHS_ALLOCATED_RANGES,
    _nhs_check_digit_valid,
    nhs_context_reason,
    non_nhs_reason,
)
from pystreams.risk.presidiofp.retired import RETIRED_RECOGNIZERS


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

    assert not presidiofp.reason(ENTITY_TYPE_IP_ADDRESS, "71.126.87.167"), (
        "residential IP"
    )
    assert not presidiofp.reason(ENTITY_TYPE_EMAIL_ADDRESS, "ada@speakeasy.com"), (
        "real email"
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
        "pii.us_driver_license",
    ]
    assert presidiofp.context_rule_ids() == ["pii.uk_nhs"]
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
