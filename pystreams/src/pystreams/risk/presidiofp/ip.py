"""IP false-positive catalogs and heuristics."""

import ipaddress

from .ip_asn import infra_asn_reason


def non_pii_ip_reason(s: str) -> str:
    """Return why an IP string is non-PII noise, or "" if it should be treated
    as real PII.

    Covers IANA-reserved space, well-known public DNS resolvers, common
    placeholder IPs, shape heuristics, and (as a fall-through) cloud / CDN /
    managed-hosting ASN attribution.
    """
    try:
        addr = ipaddress.ip_address(s.strip())
    except ValueError:
        return ""

    # Key the exact lookup off the parsed address's canonical form, not the raw
    # input, so equivalent spellings (uppercase hex, expanded zero groups) still
    # match the catalog.
    canonical = str(addr)
    exact = _NON_PII_IP_EXACT.get(canonical)
    if exact:
        return exact

    for network, description in _NON_PII_IP_PREFIXES:
        if addr in network:
            return description

    heuristic = _non_pii_ip_heuristic(addr)
    if heuristic:
        return heuristic

    return infra_asn_reason(canonical)


def _non_pii_ip_heuristic(
    addr: ipaddress.IPv4Address | ipaddress.IPv6Address,
) -> str:
    """Describe IPs that fit placeholder shapes not in the exact/prefix sets:
    an IPv4 ``X.0.0.0`` network address of a public /8, or an IPv6 whose 16-byte
    form has at most two non-zero bytes (``1::``, ``dead::``, ``::cafe``, ...).
    """
    if isinstance(addr, ipaddress.IPv4Address):
        b = addr.packed
        if b[1] == 0 and b[2] == 0 and b[3] == 0:
            return "network address of /8"
    else:
        nonzero = sum(1 for x in addr.packed if x != 0)
        if nonzero <= 2:
            return "sparse IPv6 likely placeholder"
    return ""


def _prefixes() -> list[tuple[ipaddress.IPv4Network | ipaddress.IPv6Network, str]]:
    raw = [
        ("10.0.0.0/8", "RFC1918 private space"),
        ("172.16.0.0/12", "RFC1918 private space"),
        ("192.168.0.0/16", "RFC1918 private space"),
        ("127.0.0.0/8", "loopback addresses"),
        ("::1/128", "loopback addresses"),
        ("169.254.0.0/16", "link-local addresses"),
        ("fe80::/10", "link-local addresses"),
        ("224.0.0.0/4", "multicast address space"),
        ("ff00::/8", "multicast address space"),
        ("100.64.0.0/10", "CGNAT shared space RFC6598"),
        ("192.0.2.0/24", "documentation range RFC5737"),
        ("198.51.100.0/24", "documentation range RFC5737"),
        ("203.0.113.0/24", "documentation range RFC5737"),
        ("2001:db8::/32", "documentation range RFC5737"),
        ("192.88.99.0/24", "6to4 deprecated anycast"),
        ("240.0.0.0/4", "class E reserved"),
        ("0.0.0.0/8", "this network RFC791"),
        ("198.18.0.0/15", "benchmarking range RFC2544"),
    ]
    return [(ipaddress.ip_network(cidr), desc) for cidr, desc in raw]


# IANA-reserved address space that should never be treated as PII.
_NON_PII_IP_PREFIXES = _prefixes()

# Individual addresses to ignore: well-known public DNS resolvers plus a small
# set of widely-used placeholder IPs. Keys are canonical IP strings.
_NON_PII_IP_EXACT: dict[str, str] = {
    # Limited broadcast
    "255.255.255.255": "limited broadcast",
    # Cloudflare 1.1.1.1 family
    "1.0.0.1": "Cloudflare 1.1.1.1 resolver",
    "1.0.0.2": "Cloudflare 1.1.1.1 resolver",
    "1.0.0.3": "Cloudflare 1.1.1.1 resolver",
    "1.1.1.1": "Cloudflare 1.1.1.1 resolver",
    "1.1.1.2": "Cloudflare 1.1.1.1 resolver",
    "1.1.1.3": "Cloudflare 1.1.1.1 resolver",
    "2606:4700:4700::64": "Cloudflare 1.1.1.1 resolver",
    "2606:4700:4700::1001": "Cloudflare 1.1.1.1 resolver",
    "2606:4700:4700::1002": "Cloudflare 1.1.1.1 resolver",
    "2606:4700:4700::1003": "Cloudflare 1.1.1.1 resolver",
    "2606:4700:4700::1111": "Cloudflare 1.1.1.1 resolver",
    "2606:4700:4700::1112": "Cloudflare 1.1.1.1 resolver",
    "2606:4700:4700::1113": "Cloudflare 1.1.1.1 resolver",
    "2606:4700:4700::6400": "Cloudflare 1.1.1.1 resolver",
    # Google Public DNS
    "8.8.4.4": "Google public DNS",
    "8.8.8.8": "Google public DNS",
    "2001:4860:4860::64": "Google public DNS",
    "2001:4860:4860::6464": "Google public DNS",
    "2001:4860:4860::8844": "Google public DNS",
    "2001:4860:4860::8888": "Google public DNS",
    # OpenDNS / Cisco
    "208.67.220.123": "OpenDNS Cisco resolver",
    "208.67.220.220": "OpenDNS Cisco resolver",
    "208.67.220.222": "OpenDNS Cisco resolver",
    "208.67.222.123": "OpenDNS Cisco resolver",
    "208.67.222.220": "OpenDNS Cisco resolver",
    "208.67.222.222": "OpenDNS Cisco resolver",
    "2620:0:ccc::2": "OpenDNS Cisco resolver",
    "2620:0:ccd::2": "OpenDNS Cisco resolver",
    "2620:119:35::35": "OpenDNS Cisco resolver",
    "2620:119:53::53": "OpenDNS Cisco resolver",
    # Quad9
    "9.9.9.9": "Quad9 secure DNS",
    "9.9.9.10": "Quad9 secure DNS",
    "9.9.9.11": "Quad9 secure DNS",
    "149.112.112.9": "Quad9 secure DNS",
    "149.112.112.10": "Quad9 secure DNS",
    "149.112.112.11": "Quad9 secure DNS",
    "149.112.112.112": "Quad9 secure DNS",
    "2620:fe::9": "Quad9 secure DNS",
    "2620:fe::10": "Quad9 secure DNS",
    "2620:fe::11": "Quad9 secure DNS",
    "2620:fe::fe": "Quad9 secure DNS",
    "2620:fe::fe:9": "Quad9 secure DNS",
    "2620:fe::fe:10": "Quad9 secure DNS",
    "2620:fe::fe:11": "Quad9 secure DNS",
    # AdGuard
    "94.140.14.14": "AdGuard filtering DNS",
    "94.140.14.15": "AdGuard filtering DNS",
    "94.140.14.140": "AdGuard filtering DNS",
    "94.140.14.141": "AdGuard filtering DNS",
    "94.140.15.15": "AdGuard filtering DNS",
    "94.140.15.16": "AdGuard filtering DNS",
    "2a10:50c0::1:ff": "AdGuard filtering DNS",
    "2a10:50c0::2:ff": "AdGuard filtering DNS",
    "2a10:50c0::ad1:ff": "AdGuard filtering DNS",
    "2a10:50c0::ad2:ff": "AdGuard filtering DNS",
    "2a10:50c0::bad1:ff": "AdGuard filtering DNS",
    "2a10:50c0::bad2:ff": "AdGuard filtering DNS",
    # CleanBrowsing
    "185.228.168.9": "CleanBrowsing filtering DNS",
    "185.228.168.10": "CleanBrowsing filtering DNS",
    "185.228.168.168": "CleanBrowsing filtering DNS",
    "185.228.169.9": "CleanBrowsing filtering DNS",
    "185.228.169.11": "CleanBrowsing filtering DNS",
    "185.228.169.168": "CleanBrowsing filtering DNS",
    # Control D
    "76.76.2.0": "Control D customisable DNS",
    "76.76.10.0": "Control D customisable DNS",
    # Yandex
    "77.88.8.1": "Yandex public DNS",
    "77.88.8.2": "Yandex public DNS",
    "77.88.8.3": "Yandex public DNS",
    "77.88.8.7": "Yandex public DNS",
    "77.88.8.8": "Yandex public DNS",
    "2a02:6b8::feed:ff": "Yandex public DNS",
    "2a02:6b8::feed:a11": "Yandex public DNS",
    "2a02:6b8::feed:bad": "Yandex public DNS",
    "2a02:6b8:0:1::feed:ff": "Yandex public DNS",
    "2a02:6b8:0:1::feed:a11": "Yandex public DNS",
    "2a02:6b8:0:1::feed:bad": "Yandex public DNS",
    # Vercara / UltraDNS (includes inherited Norton ConnectSafe IPs)
    "64.6.64.6": "Vercara UltraDNS resolver",
    "64.6.65.6": "Vercara UltraDNS resolver",
    "156.154.70.1": "Vercara UltraDNS resolver",
    "156.154.70.2": "Vercara UltraDNS resolver",
    "156.154.70.3": "Vercara UltraDNS resolver",
    "156.154.70.4": "Vercara UltraDNS resolver",
    "156.154.70.5": "Vercara UltraDNS resolver",
    "156.154.71.1": "Vercara UltraDNS resolver",
    "156.154.71.2": "Vercara UltraDNS resolver",
    "156.154.71.3": "Vercara UltraDNS resolver",
    "156.154.71.4": "Vercara UltraDNS resolver",
    "156.154.71.5": "Vercara UltraDNS resolver",
    "199.85.126.10": "Vercara UltraDNS resolver",
    "199.85.127.10": "Vercara UltraDNS resolver",
    # Lumen / Level3 carrier DNS
    "4.2.2.1": "Lumen Level3 carrier DNS",
    "4.2.2.2": "Lumen Level3 carrier DNS",
    "4.2.2.3": "Lumen Level3 carrier DNS",
    "4.2.2.4": "Lumen Level3 carrier DNS",
    "4.2.2.5": "Lumen Level3 carrier DNS",
    "4.2.2.6": "Lumen Level3 carrier DNS",
    "205.171.3.66": "Lumen Level3 carrier DNS",
    "205.171.202.166": "Lumen Level3 carrier DNS",
    "209.244.0.3": "Lumen Level3 carrier DNS",
    "209.244.0.4": "Lumen Level3 carrier DNS",
    # Oracle Dyn
    "216.146.35.35": "Oracle Dyn public DNS",
    "216.146.36.36": "Oracle Dyn public DNS",
    # CIRA Canadian Shield
    "149.112.121.10": "CIRA Canadian Shield",
    "149.112.122.10": "CIRA Canadian Shield",
    # DNSforFamily (Hetzner-hosted)
    "78.47.64.161": "DNSforFamily on Hetzner",
    "94.130.180.225": "DNSforFamily on Hetzner",
    # FlashStart
    "185.236.104.104": "FlashStart filtering DNS",
    "185.236.105.105": "FlashStart filtering DNS",
    # Misc niche resolvers
    "185.222.222.222": "DNS.SB privacy resolver",
    "2001:558:feed::1": "Comcast Xfinity ISP DNS",
    "2001:558:feed::2": "Comcast Xfinity ISP DNS",
    "223.5.5.5": "Alibaba AliDNS public",
    "223.6.6.6": "Alibaba AliDNS public",
    "119.29.29.29": "Tencent DNSPod public",
    "182.254.116.116": "Tencent DNSPod public",
    "180.76.76.76": "Baidu public DNS",
    # Common placeholder / literal-example IPs surfaced repeatedly by the offline
    # FP classifier. All resolve to AS0 ("unrouted") in DB-IP, so the ASN
    # fall-through cannot catch them and the explicit list is load-bearing.
    "1.2.3.4": "common placeholder address",
    "1.2.1.1": "common placeholder address",
    "1.3.86.78": "common placeholder address",
    "2.1.2.3": "common placeholder address",
    "2.2.2.2": "common placeholder address",
    "2.2.7.3": "common placeholder address",
    "25.8.3.66": "common placeholder address",
    "9.3.15.0": "common placeholder address",
    "133.0.0.0": "common placeholder address",
    "45.1.37.1": "common placeholder address",
}
