"""Email false-positive catalogs.

See ``non_pii_email_reason`` for the rationale behind each layer.
"""


def non_pii_email_reason(s: str) -> str:
    """Return why an email-shaped string is non-PII noise, or "" if it could be real.

    Five layers, in order:

      1. Exact-match against ``KNOWN_FP_EMAILS`` — one-off addresses (e.g. the
         ``git@github.com`` SSH pseudo-user).
      2. Reserved / placeholder / image-extension domain (see
         ``_placeholder_domain_reason``).
      3. Any ``/`` in the candidate — in practice a URL or path fragment.
      4. Trailing digit on the right of the final ``@`` — a version suffix like
         ``pkg@v1.2.3`` rather than an email (TLDs are letters).
      5. Local-parts that can never identify a real person (template tokens and
         the automated ``noreply`` / ``no-reply`` aliases).
    """
    trimmed = s.strip()
    if not trimmed:
        return ""
    lower = trimmed.lower()

    if lower in KNOWN_FP_EMAILS:
        return "known false-positive address"

    reason = _placeholder_domain_reason(lower)
    if reason:
        return reason

    if "/" in trimmed:
        return "contains '/' (URL or path fragment)"

    at = trimmed.rfind("@")
    if 0 <= at < len(trimmed) - 1 and trimmed[-1].isdigit():
        return "domain ends in digit (likely version suffix)"

    at = lower.rfind("@")
    if at > 0 and lower[:at] in PLACEHOLDER_LOCAL_PARTS:
        return "fixture / placeholder local-part"

    return ""


def _placeholder_domain_reason(lower: str) -> str:
    """Report whether the right-hand side of the final ``@`` is a reserved,
    image-extension, or fixture domain. ``lower`` is the lowercased input.
    """
    at = lower.rfind("@")
    if at < 0 or at >= len(lower) - 1:
        return ""
    parts = lower[at + 1 :].split(".")
    tld = parts[-1]
    if tld in RESERVED_SPECIAL_TLDS:
        return "RFC 6761 reserved special-use TLD"
    if tld in IMAGE_FILE_EXTENSION_TLDS:
        return "image file extension (URL fragment)"
    if len(parts) < 2:
        return ""
    sld = parts[-2]
    if sld in PLACEHOLDER_SLDS and tld in PLACEHOLDER_TLDS:
        return "fixture / placeholder domain"
    return ""


# Top-level domains reserved by RFC 6761 for special use. Anything ending in one
# of these labels is guaranteed not to resolve to a public mailbox.
RESERVED_SPECIAL_TLDS = frozenset({"example", "invalid", "localhost", "test"})

# File extensions Presidio occasionally mis-shapes as TLDs when an
# ``@2x.png``-style asset URL filename leaks out without the leading ``/``.
IMAGE_FILE_EXTENSION_TLDS = frozenset({"png", "svg", "jpg", "jpeg", "gif"})

# Second-level domains conventionally used for fixtures and obviously-fake
# corporate examples.
PLACEHOLDER_SLDS = frozenset(
    {
        "example",
        "test",
        "asdf",
        "fake",
        "nowhere",
        "placeholder",
        "sample",
        "dummy",
        "yourorg",
        "acme",
        "acmecorp",
        "acmestore",
    }
)

# Top-level domains placeholder SLDs commonly appear under.
PLACEHOLDER_TLDS = frozenset({"com", "org", "net", "local", "dev", "io"})

# Complete email-shaped strings that are always false positives but don't fit
# any structural layer. Matched as an exact lowercase comparison.
KNOWN_FP_EMAILS = frozenset({"git@github.com"})

# Local-parts that can never identify a real person: template tokens and the
# universally automated ``noreply`` / ``no-reply`` aliases. Canonical
# placeholder person names (``john.doe`` etc.) are deliberately excluded.
PLACEHOLDER_LOCAL_PARTS = frozenset(
    {"first.last", "firstname.lastname", "noreply", "no-reply"}
)
