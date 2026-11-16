"""Check extracted PEM trust bundles against the incident's affected root hashes.

Usage: python check-emudhra-ca.py /path/to/ca-certificates.crt [...]
This checks bundle contents, not a client's choice of trust store or revocation.
"""

import hashlib
import re
import ssl
import sys
from pathlib import Path

# https://emudhra.we-have-a-problem.com/roots.json (2026-10-20)
BLOCKED = {
    "7dd78d5f4f13459a83dff9abbba62edbaf6f2d102bf257fd712f4d9f2746ed8d": "emSign Root TLS CA - G3",
    "f2edaaff3d9b701fb4d14c9f285f8c0bad90fc3bcc1fa07d47f4718edaba34a5": "emSign Root CS CA - G3",
    "d7a7a0fb5d7e2731d771e9484ebcdef71d5f0c3e0a2948782bc83ee0ea699ef4": "AAA Certificate Services",
    "86a1ecba089c4a8d3bbe2734c612ba341d813e043cf9e8a862cd5c57a36bbe6b": "emSign ECC Root CA - G3",
}


def main() -> int:
    if len(sys.argv) < 2:
        print(__doc__, file=sys.stderr)
        return 2

    failed = False
    for filename in sys.argv[1:]:
        text = Path(filename).read_text()
        certificates = re.findall(
            r"-----BEGIN CERTIFICATE-----.*?-----END CERTIFICATE-----", text, re.DOTALL
        )
        if not certificates:
            print(f"FAIL {filename}: no certificates found")
            failed = True
            continue

        hashes = {
            hashlib.sha256(ssl.PEM_cert_to_DER_cert(pem)).hexdigest()
            for pem in certificates
        }
        blocked = sorted(
            BLOCKED[fingerprint] for fingerprint in hashes & BLOCKED.keys()
        )
        if blocked:
            print(f"FAIL {filename}: trusts {', '.join(blocked)}")
            failed = True
        else:
            print(f"PASS {filename}: {len(hashes)} certificates, no affected roots")

    return int(failed)


if __name__ == "__main__":
    sys.exit(main())
