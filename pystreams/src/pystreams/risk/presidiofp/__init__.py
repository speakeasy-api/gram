"""Presidio false-positive classification.

Classifies Presidio PII findings (reserved/placeholder IPs and emails, cloud/CDN
ASN attribution, NHS number validity, retired recognizers) so the streaming
scanner can drop the noise before publishing.
"""

from .classify import (
    context_rule_ids,
    reason,
    reason_by_rule_id,
    reason_by_rule_id_in_context,
    reason_in_context,
    rule_ids,
)

__all__ = [
    "context_rule_ids",
    "reason",
    "reason_by_rule_id",
    "reason_by_rule_id_in_context",
    "reason_in_context",
    "rule_ids",
]
