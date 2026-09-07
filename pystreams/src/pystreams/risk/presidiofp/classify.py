"""Classifies Presidio PII findings as false positives.

Holds the per-entity false-positive catalogs (reserved/placeholder IPs and
emails, cloud/CDN ASN attribution, NHS number validity, retired recognizers) and
the dispatch over them.
"""

from .email import non_pii_email_reason
from .ip import non_pii_ip_reason
from .nhs import nhs_context_reason, non_nhs_reason
from .retired import RETIRED_RECOGNIZERS, retired_reason

# Presidio's UPPER_SNAKE entity names that have a false-positive catalog.
ENTITY_TYPE_EMAIL_ADDRESS = "EMAIL_ADDRESS"
ENTITY_TYPE_IP_ADDRESS = "IP_ADDRESS"
ENTITY_TYPE_UK_NHS = "UK_NHS"

# The canonical rule_id prefix for Presidio PII findings. Mirrors
# ``_canonical_rule_id`` in the handler ("pii." + lowercased entity); keep in sync.
_RULE_PREFIX = "pii."


def reason(entity_type: str, match: str) -> str:
    """Return the catalog reason a Presidio match is treated as noise, or "" when
    it is a real finding. Entity types without a catalog always return "".

    This is the value-only view: it judges the matched text on its own. Callers
    that hold the text the match was found in should prefer ``reason_in_context``,
    which additionally applies the catalogs that need surrounding text.
    """
    return reason_in_context(entity_type, match, "")


def reason_in_context(entity_type: str, match: str, text: str) -> str:
    """``reason`` with the payload the match was found in. Passing an empty
    ``text`` is equivalent to calling ``reason``: no finding is ever suppressed
    for missing context, only for context that is present and carries no signal.
    """
    retired = retired_reason(entity_type)
    if retired:
        return retired
    if entity_type == ENTITY_TYPE_IP_ADDRESS:
        return non_pii_ip_reason(match.strip())
    if entity_type == ENTITY_TYPE_EMAIL_ADDRESS:
        return non_pii_email_reason(match)
    if entity_type == ENTITY_TYPE_UK_NHS:
        return non_nhs_reason(match) or nhs_context_reason(text)
    return ""


def reason_by_rule_id(rule_id: str, match: str) -> str:
    """``reason`` keyed by a stored finding's canonical rule_id (e.g.
    ``pii.ip_address``), for re-evaluating persisted findings. Rule ids outside
    the catalogs always return "".
    """
    return reason(_entity_type_for_rule_id(rule_id), match)


def reason_by_rule_id_in_context(rule_id: str, match: str, text: str) -> str:
    """``reason_in_context`` keyed by a stored finding's canonical rule_id."""
    return reason_in_context(_entity_type_for_rule_id(rule_id), match, text)


def rule_ids() -> list[str]:
    """Return the canonical rule ids whose entity types have a catalog. Keep in
    sync with the dispatch in ``reason_in_context``.
    """
    return [
        _rule_id_for_entity(ENTITY_TYPE_IP_ADDRESS),
        _rule_id_for_entity(ENTITY_TYPE_EMAIL_ADDRESS),
        _rule_id_for_entity(ENTITY_TYPE_UK_NHS),
    ] + [_rule_id_for_entity(entity) for entity in sorted(RETIRED_RECOGNIZERS)]


def context_rule_ids() -> list[str]:
    """Return the subset of ``rule_ids`` whose classification can change once the
    surrounding text is supplied.
    """
    return [_rule_id_for_entity(ENTITY_TYPE_UK_NHS)]


def _rule_id_for_entity(entity: str) -> str:
    return _RULE_PREFIX + entity.lower()


def _entity_type_for_rule_id(rule_id: str) -> str:
    """Invert ``_rule_id_for_entity``: ``pii.ip_address`` -> ``IP_ADDRESS``.
    Returns "" for non-PII rule ids.
    """
    if not rule_id.startswith(_RULE_PREFIX):
        return ""
    return rule_id[len(_RULE_PREFIX) :].upper()
