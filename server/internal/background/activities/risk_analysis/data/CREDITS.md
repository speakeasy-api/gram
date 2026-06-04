# Third-party data attribution

## dbip-asn.mmdb

IP-to-ASN database derived from
[DB-IP IP-to-ASN Lite](https://db-ip.com/db/lite.php), republished as MMDB by
[sapics/ip-location-db](https://github.com/sapics/ip-location-db/tree/main/dbip-asn-mmdb).

License: [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/).

Per the DB-IP attribution requirement, downstream UI surfaces that display
results derived from this file should include:

> IP Geolocation by DB-IP, https://db-ip.com/

## Refresh

The checked-in snapshot is pinned by sha256 in `dbip-asn.mmdb.sha256`.
Run the refresh task instead of curling directly so the new file is
checksummed and the package tests run against it:

```
mise run gen:risk-asn-mmdb
```

The task downloads the latest snapshot from sapics' GitHub raw URL into
a temp file, recomputes the sha256, writes both the mmdb and the
checksum file, then runs `go test ./server/internal/background/activities/risk_analysis/...`
to fail fast if a known-cloud lookup stops resolving (the test suite
asserts Cloudflare AS13335, Google LLC AS15169, GitHub AS36459, and
Fastly AS54113 still classify as infra).

Upstream refreshes monthly.
