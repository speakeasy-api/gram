"""Tests for the presidiofp false-positive classifier.

Mirrors the Go unit tests in
``server/internal/risk/presidiofp/classify_test.go``
(``TestNonPIIIPExactKeysAreCanonical``, ``TestReason``, ``TestReasonByRuleID``)
and adds broader table coverage of the IP and email catalogs.

(``fp_split_test.go`` is build-tagged dev tooling that regenerates testdata
rather than a unit test, so it has no counterpart here.)
"""

import ipaddress

import pytest

from pystreams.risk import presidiofp
from pystreams.risk.presidiofp import ip_asn
from pystreams.risk.presidiofp.classify import (
    ENTITY_TYPE_EMAIL_ADDRESS,
    ENTITY_TYPE_IP_ADDRESS,
    _entity_type_for_rule_id,
)
from pystreams.risk.presidiofp.ip import _NON_PII_IP_EXACT


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
    assert presidiofp.rule_ids() == ["pii.ip_address", "pii.email_address"]
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
