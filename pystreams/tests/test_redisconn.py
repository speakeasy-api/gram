"""Tests for connecting to a Redis or Valkey endpoint with TLS and IAM."""

from __future__ import annotations

import asyncio
import datetime
import ipaddress
import ssl
import threading
from collections.abc import AsyncIterator, Sequence
from dataclasses import dataclass, field
from pathlib import Path

import anyio
import google.auth.transport.requests
import pytest
from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import ec
from cryptography.x509.oid import NameOID
from redis.exceptions import ConnectionError as RedisConnectionError

from pystreams.deps.redisconn import (
    IAM_USERNAME,
    TOKEN_EARLY_REFRESH,
    AuthMode,
    Endpoint,
    GoogleIAMCredentialProvider,
    TokenOverPlaintextError,
    client,
)

_TOKEN = "access-token-1"
_PASSWORD = "secret"


# --- certificates ------------------------------------------------------------


@dataclass
class _CA:
    pem: str
    cert: x509.Certificate
    key: ec.EllipticCurvePrivateKey


def _name(common_name: str) -> x509.Name:
    return x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, common_name)])


def _new_ca() -> _CA:
    key = ec.generate_private_key(ec.SECP256R1())
    now = datetime.datetime.now(datetime.UTC)
    cert = (
        x509.CertificateBuilder()
        .subject_name(_name("redis test CA"))
        .issuer_name(_name("redis test CA"))
        .public_key(key.public_key())
        .serial_number(x509.random_serial_number())
        .not_valid_before(now - datetime.timedelta(hours=1))
        .not_valid_after(now + datetime.timedelta(hours=1))
        .add_extension(x509.BasicConstraints(ca=True, path_length=None), critical=True)
        .add_extension(
            x509.KeyUsage(
                digital_signature=False,
                content_commitment=False,
                key_encipherment=False,
                data_encipherment=False,
                key_agreement=False,
                key_cert_sign=True,
                crl_sign=True,
                encipher_only=False,
                decipher_only=False,
            ),
            critical=True,
        )
        .add_extension(
            x509.SubjectKeyIdentifier.from_public_key(key.public_key()), critical=False
        )
        .sign(key, hashes.SHA256())
    )
    pem = cert.public_bytes(serialization.Encoding.PEM).decode()
    return _CA(pem=pem, cert=cert, key=key)


def _server_context(
    ca: _CA, tmp_path: Path, sans: Sequence[x509.GeneralName]
) -> ssl.SSLContext:
    key = ec.generate_private_key(ec.SECP256R1())
    now = datetime.datetime.now(datetime.UTC)
    cert = (
        x509.CertificateBuilder()
        .subject_name(_name("redis test server"))
        .issuer_name(ca.cert.subject)
        .public_key(key.public_key())
        .serial_number(x509.random_serial_number())
        .not_valid_before(now - datetime.timedelta(hours=1))
        .not_valid_after(now + datetime.timedelta(hours=1))
        .add_extension(x509.SubjectAlternativeName(sans), critical=False)
        .add_extension(
            x509.ExtendedKeyUsage([x509.oid.ExtendedKeyUsageOID.SERVER_AUTH]),
            critical=False,
        )
        .add_extension(
            x509.AuthorityKeyIdentifier.from_issuer_public_key(ca.key.public_key()),
            critical=False,
        )
        .sign(ca.key, hashes.SHA256())
    )
    cert_path = tmp_path / "server.pem"
    key_path = tmp_path / "server.key"
    cert_path.write_bytes(cert.public_bytes(serialization.Encoding.PEM))
    key_path.write_bytes(
        key.private_bytes(
            serialization.Encoding.PEM,
            serialization.PrivateFormat.PKCS8,
            serialization.NoEncryption(),
        )
    )
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.load_cert_chain(cert_path, key_path)
    return context


_LOOPBACK = [x509.IPAddress(ipaddress.ip_address("127.0.0.1"))]


# --- a minimal RESP server -----------------------------------------------------


@dataclass
class _Server:
    """Answers every command with OK (PING with PONG) and records them."""

    context: ssl.SSLContext | None
    commands: list[list[bytes]] = field(default_factory=list)
    addr: str = ""

    async def _handle(
        self, reader: asyncio.StreamReader, writer: asyncio.StreamWriter
    ) -> None:
        try:
            while True:
                header = await reader.readline()
                if not header:
                    return
                count = int(header[1:])
                args: list[bytes] = []
                for _ in range(count):
                    size = int((await reader.readline())[1:])
                    args.append((await reader.readexactly(size + 2))[:-2])
                self.commands.append(args)
                name = args[0].upper()
                if name == b"PING":
                    writer.write(b"+PONG\r\n")
                elif name == b"HELLO":
                    writer.write(b"%1\r\n$5\r\nproto\r\n:" + args[1] + b"\r\n")
                else:
                    writer.write(b"+OK\r\n")
                await writer.drain()
        except ConnectionError, asyncio.IncompleteReadError, ssl.SSLError:
            return
        finally:
            writer.close()

    def auths(self) -> list[list[bytes]]:
        """Credentials sent by AUTH, or by HELLO's AUTH option."""
        sent: list[list[bytes]] = []
        for command in self.commands:
            name = command[0].upper()
            if name == b"AUTH":
                sent.append(command[1:])
            elif name == b"HELLO" and b"AUTH" in (a.upper() for a in command):
                at = [a.upper() for a in command].index(b"AUTH")
                sent.append(command[at + 1 : at + 3])
        return sent


@pytest.fixture
def ca() -> _CA:
    return _new_ca()


@pytest.fixture
def server_context(ca: _CA, tmp_path: Path) -> ssl.SSLContext:
    return _server_context(ca, tmp_path, _LOOPBACK)


async def _serve(context: ssl.SSLContext | None) -> AsyncIterator[_Server]:
    server = _Server(context=context)
    listener = await asyncio.start_server(server._handle, "127.0.0.1", 0, ssl=context)
    port = listener.sockets[0].getsockname()[1]
    server.addr = f"127.0.0.1:{port}"
    async with listener:
        yield server


@pytest.fixture
async def tls_server(server_context: ssl.SSLContext) -> AsyncIterator[_Server]:
    async for server in _serve(server_context):
        yield server


@pytest.fixture
async def plain_server() -> AsyncIterator[_Server]:
    async for server in _serve(None):
        yield server


# --- a fake google-auth credential ---------------------------------------------


class _FakeCredentials:
    """Issues numbered tokens; records the thread each refresh ran on."""

    def __init__(self, lifetime: datetime.timedelta, marker: Path) -> None:
        self.token: str | None = None
        self.expiry: datetime.datetime | None = None
        self.refreshes = 0
        self.refresh_threads: list[int] = []
        self._lifetime = lifetime
        self._marker = marker

    def refresh(self, request: object) -> None:
        # Real refreshes do network I/O; opening a file stands in for it so the
        # suite's blocking detector would fail the test if this ran on the loop.
        with self._marker.open("a") as f:
            f.write("refresh\n")
        self.refresh_threads.append(threading.get_ident())
        self.refreshes += 1
        self.token = f"access-token-{self.refreshes}"
        self.expiry = (
            datetime.datetime.now(datetime.UTC).replace(tzinfo=None) + self._lifetime
        )


def _provider(credentials: _FakeCredentials) -> GoogleIAMCredentialProvider:
    return GoogleIAMCredentialProvider(
        credentials, request=google.auth.transport.requests.Request()
    )


# --- tests -----------------------------------------------------------------------


@pytest.mark.parametrize(
    ("endpoint", "message"),
    [
        (Endpoint(addr="", auth=AuthMode.NONE), "host:port"),
        (Endpoint(addr="10.0.0.1", auth=AuthMode.NONE), "host:port"),
        (Endpoint(addr="::1:6379", auth=AuthMode.NONE), "brackets"),
        (Endpoint(addr="10.0.0.1:6379", auth=AuthMode.PASSWORD), "has none"),
        (
            Endpoint(addr="10.0.0.1:6379", auth=AuthMode.NONE, password=_PASSWORD),
            "auth mode is none",
        ),
        (
            Endpoint(
                addr="10.0.0.1:6379",
                auth=AuthMode.IAM,
                password=_PASSWORD,
                ca_cert_pem="pem",
            ),
            "auth mode is iam",
        ),
    ],
)
def test_validate_rejects(endpoint: Endpoint, message: str) -> None:
    with pytest.raises(ValueError, match=message):
        endpoint.validate()


def test_iam_requires_tls() -> None:
    with pytest.raises(TokenOverPlaintextError):
        Endpoint(addr="10.0.0.1:6379", auth=AuthMode.IAM).validate()


def test_host_port() -> None:
    assert Endpoint(addr="10.0.0.1:6380", auth=AuthMode.NONE).host_port() == (
        "10.0.0.1",
        6380,
    )
    assert Endpoint(addr="[::1]:6379", auth=AuthMode.NONE).host_port() == ("::1", 6379)


def test_with_password() -> None:
    assert Endpoint.with_password("10.0.0.1:6379", _PASSWORD).auth is AuthMode.PASSWORD
    assert Endpoint.with_password("10.0.0.1:6379", None).auth is AuthMode.NONE
    assert Endpoint.with_password("10.0.0.1:6379", "").auth is AuthMode.NONE


def test_ssl_context_trusts_only_the_endpoint_ca(ca: _CA) -> None:
    endpoint = Endpoint(addr="10.0.0.7:6379", auth=AuthMode.NONE, ca_cert_pem=ca.pem)
    context = endpoint.ssl_context()
    assert context is not None
    assert context.verify_mode == ssl.CERT_REQUIRED
    assert context.check_hostname
    assert context.minimum_version == ssl.TLSVersion.TLSv1_2
    assert len(context.get_ca_certs()) == 1
    assert endpoint.verified_name() == "10.0.0.7"
    assert Endpoint.with_password("10.0.0.7:6379", _PASSWORD).ssl_context() is None


def test_client_requires_a_provider_for_iam(ca: _CA) -> None:
    endpoint = Endpoint(addr="10.0.0.7:6379", auth=AuthMode.IAM, ca_cert_pem=ca.pem)
    with pytest.raises(ValueError, match="no credential provider"):
        client(endpoint)


async def test_iam_client_authenticates_over_verified_tls(
    ca: _CA, tls_server: _Server, tmp_path: Path
) -> None:
    credentials = _FakeCredentials(datetime.timedelta(hours=1), tmp_path / "marker")
    endpoint = Endpoint(addr=tls_server.addr, auth=AuthMode.IAM, ca_cert_pem=ca.pem)
    redis = client(endpoint, credential_provider=_provider(credentials))
    try:
        assert await redis.ping()
    finally:
        await redis.aclose()

    assert tls_server.auths() == [[IAM_USERNAME.encode(), b"access-token-1"]]
    assert credentials.refresh_threads
    assert threading.get_ident() not in credentials.refresh_threads, (
        "the token refresh must run off the event loop thread"
    )


async def test_tls_client_refuses_a_server_signed_by_another_ca(
    tls_server: _Server, tmp_path: Path
) -> None:
    credentials = _FakeCredentials(datetime.timedelta(hours=1), tmp_path / "marker")
    other = _new_ca()
    endpoint = Endpoint(addr=tls_server.addr, auth=AuthMode.IAM, ca_cert_pem=other.pem)
    redis = client(endpoint, credential_provider=_provider(credentials))
    try:
        with pytest.raises(RedisConnectionError):
            await redis.ping()
    finally:
        await redis.aclose()

    assert tls_server.auths() == []
    assert credentials.refreshes == 0, (
        "no token is fetched before the server is verified"
    )


async def test_tls_client_refuses_a_certificate_for_another_name(
    ca: _CA, tls_server: _Server, tmp_path: Path
) -> None:
    credentials = _FakeCredentials(datetime.timedelta(hours=1), tmp_path / "marker")
    endpoint = Endpoint(
        addr=tls_server.addr,
        auth=AuthMode.IAM,
        ca_cert_pem=ca.pem,
        server_name="10.9.9.9",
    )
    redis = client(endpoint, credential_provider=_provider(credentials))
    try:
        with pytest.raises(RedisConnectionError):
            await redis.ping()
    finally:
        await redis.aclose()

    assert tls_server.auths() == []


async def test_tls_client_verifies_a_dns_server_name(ca: _CA, tmp_path: Path) -> None:
    context = _server_context(ca, tmp_path, [x509.DNSName("redis.internal.test")])
    async for server in _serve(context):
        endpoint = Endpoint(
            addr=server.addr,
            auth=AuthMode.NONE,
            ca_cert_pem=ca.pem,
            server_name="redis.internal.test",
        )
        redis = client(endpoint)
        try:
            assert await redis.ping()
        finally:
            await redis.aclose()


async def test_password_client(plain_server: _Server) -> None:
    redis = client(Endpoint.with_password(plain_server.addr, _PASSWORD))
    try:
        assert await redis.ping()
    finally:
        await redis.aclose()

    # redis-py sends a lone password as the default user's.
    assert plain_server.auths() == [[b"default", _PASSWORD.encode()]]


async def test_provider_reuses_a_token_far_from_expiry(tmp_path: Path) -> None:
    credentials = _FakeCredentials(datetime.timedelta(hours=1), tmp_path / "marker")
    provider = _provider(credentials)

    assert await provider.get_credentials_async() == (IAM_USERNAME, "access-token-1")
    assert await provider.get_credentials_async() == (IAM_USERNAME, "access-token-1")
    assert credentials.refreshes == 1


async def test_provider_refreshes_before_expiry(tmp_path: Path) -> None:
    lifetime = TOKEN_EARLY_REFRESH - datetime.timedelta(minutes=1)
    credentials = _FakeCredentials(lifetime, tmp_path / "marker")
    provider = _provider(credentials)

    assert await provider.get_credentials_async() == (IAM_USERNAME, "access-token-1")
    assert await provider.get_credentials_async() == (IAM_USERNAME, "access-token-2")


async def test_concurrent_connections_share_one_refresh(tmp_path: Path) -> None:
    credentials = _FakeCredentials(datetime.timedelta(hours=1), tmp_path / "marker")
    provider = _provider(credentials)
    results: list[tuple[str, str]] = []

    async def fetch() -> None:
        results.append(await provider.get_credentials_async())

    async with anyio.create_task_group() as tg:
        for _ in range(20):
            tg.start_soon(fetch)

    assert credentials.refreshes == 1
    assert set(results) == {(IAM_USERNAME, "access-token-1")}


def test_provider_is_async_only(tmp_path: Path) -> None:
    provider = _provider(
        _FakeCredentials(datetime.timedelta(hours=1), tmp_path / "marker")
    )
    with pytest.raises(NotImplementedError):
        provider.get_credentials()
