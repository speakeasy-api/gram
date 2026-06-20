"""Classifies Presidio PII findings as false positives.

Holds the per-entity false-positive catalogs (reserved/placeholder IPs and
emails, cloud/CDN ASN attribution) and the dispatch over them.
"""

from .email import non_pii_email_reason
from .ip import non_pii_ip_reason

# Presidio's UPPER_SNAKE entity names that have a false-positive catalog.
ENTITY_TYPE_EMAIL_ADDRESS = "EMAIL_ADDRESS"
ENTITY_TYPE_IP_ADDRESS = "IP_ADDRESS"

# The canonical rule_id prefix for Presidio PII findings. Mirrors
# ``_canonical_rule_id`` in the handler ("pii." + lowercased entity); keep in sync.
_RULE_PREFIX = "pii."


def reason(entity_type: str, match: str) -> str:
    """Return the catalog reason a Presidio match is treated as noise, or "" when
    it is a real finding. Only IP_ADDRESS and EMAIL_ADDRESS have catalogs today;
    other entity types always return "".
    """
    if entity_type == ENTITY_TYPE_IP_ADDRESS:
        return non_pii_ip_reason(match.strip())
    if entity_type == ENTITY_TYPE_EMAIL_ADDRESS:
        return non_pii_email_reason(match)
    return ""


def reason_by_rule_id(rule_id: str, match: str) -> str:
    """``reason`` keyed by a stored finding's canonical rule_id (e.g.
    ``pii.ip_address``), for re-evaluating persisted findings. Rule ids outside
    the catalogs always return "".
    """
    return reason(_entity_type_for_rule_id(rule_id), match)


def rule_ids() -> list[str]:
    """Return the canonical rule ids whose entity types have a catalog. Keep in
    sync with the dispatch in ``reason``.
    """
    return [
        _rule_id_for_entity(ENTITY_TYPE_IP_ADDRESS),
        _rule_id_for_entity(ENTITY_TYPE_EMAIL_ADDRESS),
    ]


def _rule_id_for_entity(entity: str) -> str:
    return _RULE_PREFIX + entity.lower()


def _entity_type_for_rule_id(rule_id: str) -> str:
    """Invert ``_rule_id_for_entity``: ``pii.ip_address`` -> ``IP_ADDRESS``.
    Returns "" for non-PII rule ids.
    """
    if not rule_id.startswith(_RULE_PREFIX):
        return ""
    return rule_id[len(_RULE_PREFIX) :].upper()
