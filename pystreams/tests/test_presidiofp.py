"""Tests for the presidiofp false-positive classifier."""

import ipaddress

import pytest

from pystreams.risk import presidiofp
from pystreams.risk.presidiofp.ip import _NON_PII_IP_EXACT


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
        # Common placeholder literals.
        ("1.2.3.4", True),
        ("2.2.2.2", True),
        # Shape heuristics.
        ("73.0.0.0", True),  # network address of a public /8
        ("dead::", True),  # sparse IPv6
        # Cloud / CDN / hosting via ASN lookup.
        ("52.94.236.248", True),  # Amazon AS16509
        # Real, routable, residential address — must pass through as PII.
        ("73.162.10.20", False),
    ],
)
def test_ip_reason(match: str, expect_fp: bool):
    reason = presidiofp.reason("IP_ADDRESS", match)
    assert bool(reason) is expect_fp


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
    ],
)
def test_email_reason(match: str, expect_fp: bool):
    reason = presidiofp.reason("EMAIL_ADDRESS", match)
    assert bool(reason) is expect_fp


def test_unknown_entity_type_has_no_catalog():
    assert presidiofp.reason("PERSON", "John Doe") == ""
    assert presidiofp.reason("US_SSN", "123-45-6789") == ""


def test_exact_ip_lookup_matches_non_canonical_spelling():
    # The catalog is keyed by canonical form; an expanded/uppercase spelling of a
    # catalogued resolver still resolves.
    assert presidiofp.reason("IP_ADDRESS", "2606:4700:4700:0:0:0:0:1111") != ""


def test_non_pii_ip_exact_keys_are_canonical():
    # Every exact key must already be in canonical ipaddress form, otherwise the
    # canonical-keyed lookup in non_pii_ip_reason would silently miss it.
    for key in _NON_PII_IP_EXACT:
        assert key == str(ipaddress.ip_address(key)), key


def test_reason_by_rule_id_and_rule_ids():
    assert presidiofp.rule_ids() == ["pii.ip_address", "pii.email_address"]
    assert presidiofp.reason_by_rule_id("pii.ip_address", "10.0.0.1") != ""
    assert presidiofp.reason_by_rule_id("pii.email_address", "user@example.com") != ""
    # A rule id outside the catalogs is never a false positive.
    assert presidiofp.reason_by_rule_id("pii.us_ssn", "123-45-6789") == ""
    assert presidiofp.reason_by_rule_id("not-a-pii-rule", "x") == ""
