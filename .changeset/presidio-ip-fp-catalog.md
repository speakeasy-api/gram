---
"server": patch
---

Drop a much larger class of Presidio `IP_ADDRESS` false positives. The filter now consults a unified catalog covering IANA-reserved space (RFC1918, loopback, link-local, multicast, CGNAT, documentation, 6to4 deprecated, class E, benchmarking, this-network, limited broadcast), well-known public DNS resolvers, common placeholder IPs, IPv4 `/8` network addresses and sparse IPv6 shapes, plus a cloud / CDN / managed-hosting bucket resolved against an embedded DB-IP ASN snapshot. On the production sample used to size this change (8,391 events) the new catalog suppresses about 80% of IP findings vs. ~10% under the previous filter.
