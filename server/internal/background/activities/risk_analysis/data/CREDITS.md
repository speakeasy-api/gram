# Third-party data attribution

## dbip-asn.mmdb

IP-to-ASN database derived from
[DB-IP IP-to-ASN Lite](https://db-ip.com/db/lite.php), republished as MMDB by
[sapics/ip-location-db](https://github.com/sapics/ip-location-db/tree/main/dbip-asn-mmdb).

License: [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/).

Per the DB-IP attribution requirement, downstream UI surfaces that display
results derived from this file should include:

> IP Geolocation by DB-IP — https://db-ip.com/

## Refresh

Re-download the latest snapshot with:

```
curl -L -o server/internal/background/activities/risk_analysis/data/dbip-asn.mmdb \
  https://raw.githubusercontent.com/sapics/ip-location-db/main/dbip-asn-mmdb/dbip-asn.mmdb
```

Upstream refreshes monthly.
