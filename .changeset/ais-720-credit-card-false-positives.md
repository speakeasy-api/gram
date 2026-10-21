---
"server": patch
---

Presidio credit-card detection no longer flags published test card numbers (Visa, Mastercard, Amex, Discover, Diners, JCB, UnionPay and processor sandbox cards), placeholder digit patterns, or lists of nearby four-digit ids such as consecutive PR numbers. This applies to batch scanning, realtime scanning and inline policy enforcement. Real-looking card numbers are still reported.
