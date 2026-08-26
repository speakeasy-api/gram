"""Tenant-peppered finding fingerprints, byte-identical to the Go implementation.

Mirrors ``server/internal/risk/fingerprint.go``: a per-tenant 32-byte key is
derived from the current pepper via HKDF-SHA256 (tenant id as salt, a fixed
domain-separation info string), the match is HMAC-SHA256'd under that key, and
the sum is rendered as unpadded base64url. The keyring JSON format is shared
(``{"current": "v1", "keys": {"v1": "<base64 key>"}}``), so fingerprints
produced here match the ones the Go scanners and ClickHouse rows carry.
"""

from __future__ import annotations

import base64
import hashlib
import hmac
import json
from typing import Final

# Domain-separates tenant fingerprint keys from any other use of the pepper.
# Must match hkdfInfo in server/internal/risk/fingerprint.go.
_HKDF_INFO: Final = b"gram/risk/fingerprint/tenant"
_DERIVED_KEY_LEN: Final = 32
# A pepper below this length has too little entropy to resist dictionary
# attacks on fingerprints; reject at parse time rather than fingerprint weakly.
_MIN_PEPPER_BYTES: Final = 16
# Bound on cached per-tenant derived keys; the enforcement process is
# long-lived and must not grow with tenant cardinality.
_MAX_DERIVED_CACHE: Final = 1024


class FingerprintKeyringError(ValueError):
    """The pepper keyring JSON is missing, malformed, or inconsistent."""


class Fingerprinter:
    """HMAC fingerprinting under per-tenant keys derived from a pepper keyring."""

    def __init__(self, current_version: str, keys: dict[str, bytes]) -> None:
        self._current_version = current_version
        self._keys = keys
        # Same (version, tenant) pair recurs for every finding in a message;
        # cache the HKDF output so derivation runs once per tenant.
        self._derived: dict[tuple[str, str], bytes] = {}

    def tenanted_hs256(self, tenant_id: str, message: bytes) -> tuple[bytes, str]:
        """Fingerprint message under the tenant's key for the current version."""
        key = self._derive_key(self._current_version, tenant_id)
        return hmac.new(key, message, hashlib.sha256).digest(), self._current_version

    def _derive_key(self, version: str, tenant_id: str) -> bytes:
        cache_key = (version, tenant_id)
        cached = self._derived.get(cache_key)
        if cached is not None:
            # dict preserves insertion order; re-inserting keeps hot tenants
            # out of the eviction window below.
            self._derived.pop(cache_key)
            self._derived[cache_key] = cached
            return cached
        pepper = self._keys.get(version)
        if pepper is None:
            raise FingerprintKeyringError(
                f"fingerprint pepper key not found: {version}"
            )
        key = _hkdf_sha256(
            pepper, salt=tenant_id.encode(), info=_HKDF_INFO, length=_DERIVED_KEY_LEN
        )
        while len(self._derived) >= _MAX_DERIVED_CACHE:
            self._derived.pop(next(iter(self._derived)))
        self._derived[cache_key] = key
        return key


def _hkdf_sha256(ikm: bytes, *, salt: bytes, info: bytes, length: int) -> bytes:
    """RFC 5869 HKDF over SHA-256, matching Go's ``crypto/hkdf.Key``."""
    prk = hmac.new(salt, ikm, hashlib.sha256).digest()
    okm = b""
    block = b""
    counter = 1
    while len(okm) < length:
        block = hmac.new(prk, block + info + bytes([counter]), hashlib.sha256).digest()
        okm += block
        counter += 1
    return okm[:length]


def encode_fingerprint(sum_: bytes) -> str:
    """Render a fingerprint sum as unpadded base64url (the stored encoding)."""
    return base64.urlsafe_b64encode(sum_).rstrip(b"=").decode()


def parse_pepper_keyring(raw: str | bytes) -> Fingerprinter:
    """Parse the shared keyring JSON; raises FingerprintKeyringError when invalid."""
    try:
        parsed = json.loads(raw)
    except (json.JSONDecodeError, UnicodeDecodeError) as exc:
        raise FingerprintKeyringError(
            "invalid fingerprint pepper keyring json"
        ) from exc
    if not isinstance(parsed, dict):
        raise FingerprintKeyringError("invalid fingerprint pepper keyring json")

    current = parsed.get("current")
    raw_keys = parsed.get("keys")
    if not isinstance(current, str) or not current:
        raise FingerprintKeyringError("current version not set")
    if not isinstance(raw_keys, dict) or not raw_keys:
        raise FingerprintKeyringError("no keys found in keyring")

    keys: dict[str, bytes] = {}
    for version, encoded in raw_keys.items():
        if not isinstance(version, str) or not isinstance(encoded, str):
            raise FingerprintKeyringError("invalid fingerprint pepper keyring")
        try:
            decoded = base64.b64decode(encoded, validate=True)
        except ValueError as exc:
            raise FingerprintKeyringError(
                f"failed to decode key for version {version}"
            ) from exc
        keys[version] = decoded
    if current not in keys:
        raise FingerprintKeyringError(f"current version {current} not found in keys")
    # Only the current pepper mints new fingerprints; retired keys may be any
    # length so a legacy entry cannot block startup.
    if len(keys[current]) < _MIN_PEPPER_BYTES:
        raise FingerprintKeyringError(
            f"current pepper {current} is shorter than {_MIN_PEPPER_BYTES} bytes"
        )
    return Fingerprinter(current, keys)
