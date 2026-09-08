"""Partial-mask display form of a Presidio match, mirroring the Go package.

Port of the Presidio-relevant subset of
``server/internal/risk/maskdisplay/maskdisplay.go``. Only the ``presidio``
source flows through this module, so the judge/derived-source and shadow-mcp
branches of the Go function are omitted; the email, financial, and general
tiers are byte-identical for the same input. Keep the two in lockstep.
"""

from __future__ import annotations

from typing import Final

# The Presidio email rule; its match is an email whose domain is safe to show.
_EMAIL_RULE_ID: Final = "pii.email_address"

# Presidio rule ids classified as financial (server/internal/risk/categories).
FINANCIAL_RULE_IDS: Final = frozenset(
    {"pii.credit_card", "pii.iban_code", "pii.us_bank_number", "pii.crypto"}
)


def display(rule_id: str, match: str) -> str:
    """Return the partial-mask display form of a raw Presidio match."""
    if not match:
        return ""

    if rule_id == _EMAIL_RULE_ID:
        at = match.rfind("@")
        if at >= 0:
            # Fixed three stars so the local-part length does not leak.
            return "***@" + match[at + 1 :]

    n = len(match)
    if rule_id in FINANCIAL_RULE_IDS and n >= 5:
        return "****" + match[n - 4 :]

    if n >= 8:
        return match[:4] + "*" * (n - 6) + match[n - 2 :]
    if n >= 5:
        return match[:2] + "*" * (n - 3) + match[n - 1 :]
    if n >= 3:
        return match[:1] + "*" * (n - 2) + match[n - 1 :]
    return "*" * n
