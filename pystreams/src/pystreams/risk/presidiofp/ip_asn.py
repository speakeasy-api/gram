"""Cloud / CDN / managed-hosting attribution via a DB-IP ASN lookup."""

import re
from pathlib import Path

import maxminddb
import structlog

# Matches the AS organisation names of cloud, CDN, and managed-hosting providers
# as DB-IP labels them. Goal: drop org names that belong only to infrastructure,
# never to an end user's home connection.
#
# Conservatism rules (apply when extending):
#   - Bare "google" would over-match "Google Fiber Inc." (residential ISP). Use
#     "google llc" / "google cloud" / "youtube" instead.
#   - "amazon", "microsoft", "azure", "oracle cloud", "alibaba", "tencent" are
#     kept bare because none of those companies operate a consumer ISP.
#   - Consumer-ISP brands (Verizon, Comcast, AT&T, BT, Charter, etc.) are
#     intentionally excluded: their IPs can identify an end user.
_INFRA_ASN_RE = re.compile(
    r"\b(amazon|aws|google llc|google cloud|googlebot|youtube|microsoft|azure|"
    r"cloudflare|fastly|github|akamai|digitalocean|linode|hetzner|ovh|alibaba|"
    r"tencent|baidu|vultr|the constant company|scaleway|leaseweb|hostinger|"
    r"oracle cloud|equinix|cloudfront|netlify|vercel|datacamp|m247|choopa|"
    r"phoenix nap|hurricane electric|psychz|colocrossing|sharktech|cdn|"
    r"cdnetworks|edgecast|maxcdn|keycdn|jsdelivr|stackpath|cachefly|level 3|"
    r"lumen)\b",
    re.IGNORECASE,
)

_DBIP_PATH = Path(__file__).parent / "data" / "dbip-asn.mmdb"

_reader: maxminddb.Reader | None = None
_reader_loaded = False


def _load_reader() -> maxminddb.Reader | None:
    """Open the embedded DB-IP reader once, caching the result.

    Returns ``None`` (and logs once) if the file is missing or corrupt, so
    lookups become no-ops rather than crashing.
    """
    global _reader, _reader_loaded
    if _reader_loaded:
        return _reader
    _reader_loaded = True
    try:
        _reader = maxminddb.open_database(_DBIP_PATH)
    except Exception as exc:
        structlog.get_logger().error(
            "presidiofp: failed to open DB-IP ASN database",
            error_type=type(exc).__name__,
        )
        _reader = None
    return _reader


def infra_asn_reason(addr: str) -> str:
    """Return a description if ``addr`` belongs to a cloud / CDN / hosting AS, or
    "" otherwise (including on parse / lookup failure, so the caller treats it as
    PII). ``addr`` is a canonical IP string.
    """
    reader = _load_reader()
    if reader is None:
        return ""
    try:
        rec = reader.get(addr)
    except ValueError, KeyError:
        return ""
    if not isinstance(rec, dict):
        return ""
    asn = rec.get("autonomous_system_number")
    org = rec.get("autonomous_system_organization")
    if not asn or not isinstance(org, str):
        return ""
    if not _INFRA_ASN_RE.search(org):
        return ""
    return f"AS{asn} {org} (cloud, CDN, or managed hosting)"
