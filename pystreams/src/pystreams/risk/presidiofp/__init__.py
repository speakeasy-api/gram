"""Presidio false-positive classification.

Classifies Presidio PII findings (reserved/placeholder IPs and emails, cloud/CDN
ASN attribution) so the streaming scanner can drop the noise before publishing.
"""

from .classify import reason, reason_by_rule_id, rule_ids

__all__ = ["reason", "reason_by_rule_id", "rule_ids"]
