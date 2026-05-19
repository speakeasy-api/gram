---
"server": patch
---

Drop Presidio IP_ADDRESS false positives produced from short-form IPv6 strings (`b::`, `dead::`, `1::`, …) and IPv4 unspecified `0.0.0.0`. Analysis of prod risk_results showed these single-hex-group `<hex>::` matches dominated IP_ADDRESS noise alongside the existing `::` filter; they're now dropped before becoming findings.
