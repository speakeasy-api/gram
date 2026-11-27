"""Connect to one Redis or Valkey instance.

Python counterpart of ``tunnel/redisconn``: an endpoint says where the
instance is, how a new connection authenticates (nothing, a static password, or
a Google Cloud IAM access token sent as the password of the user ``default``),
and whether the connection uses TLS. A token is only ever sent over TLS that
verifies the server against the endpoint's CA and name.

IAM tokens come from Application Default Credentials (Workload Identity on
GKE). Refreshing one makes a blocking HTTP call, so it runs in a worker thread
under an async lock: the event loop never blocks, and concurrent connections
share one refresh.
"""

from __future__ import annotations

import datetime
import ssl
from collections.abc import Callable, Mapping
from dataclasses import dataclass
from enum import StrEnum
from functools import partial
from typing import Any, Final, Protocol

import anyio
import anyio.to_thread
import google.auth
import google.auth.transport
import google.auth.transport.requests
from redis import asyncio as redis_asyncio
from redis.asyncio.connection import Connection
from redis.credentials import CredentialProvider

# The user an IAM access token authenticates as on Memorystore for Valkey.
IAM_USERNAME: Final = "default"

# The OAuth scope Memorystore requires of an IAM access token.
CLOUD_PLATFORM_SCOPE: Final = "https://www.googleapis.com/auth/cloud-platform"

# How long before its expiry a cached access token is replaced. Tokens last an
# hour and the instance checks one only when a connection authenticates, so
# five minutes leaves room for a slow metadata server and clock skew without
# refreshing often. Matches tunnel/redisconn.
TOKEN_EARLY_REFRESH: Final = datetime.timedelta(minutes=5)


class AuthMode(StrEnum):
    """How an endpoint authenticates a new connection."""

    NONE = "none"
    PASSWORD = "password"
    IAM = "iam"


class TokenOverPlaintextError(ValueError):
    """An IAM endpoint without TLS would send its token unencrypted."""


@dataclass(frozen=True, kw_only=True)
class Endpoint:
    """One instance and how to connect to it."""

    # host:port, with an IPv6 host in brackets.
    addr: str
    auth: AuthMode
    # The static password for AuthMode.PASSWORD; empty otherwise.
    password: str = ""
    # PEM certificates that sign the server certificate. When set, the
    # connection uses TLS and trusts only these; when empty it is plaintext.
    ca_cert_pem: str = ""
    # The name or IP address the server certificate must carry in its subject
    # alternative names. Defaults to the host in addr.
    server_name: str = ""

    def __post_init__(self) -> None:
        # Endpoints built from configuration may carry the mode as a plain
        # string; an unknown one is refused here rather than read as "none".
        object.__setattr__(self, "auth", AuthMode(self.auth))

    @classmethod
    def with_password(cls, addr: str, password: str | None) -> Endpoint:
        """A plaintext instance that takes password, or nothing when empty."""
        if password:
            return cls(addr=addr, auth=AuthMode.PASSWORD, password=password)
        return cls(addr=addr, auth=AuthMode.NONE)

    @property
    def tls(self) -> bool:
        return bool(self.ca_cert_pem.strip())

    def host_port(self) -> tuple[str, int]:
        """Split addr into host and port; raises ValueError when malformed."""
        host, sep, port = self.addr.rpartition(":")
        if not sep or not host or not port.isdigit():
            raise ValueError(f"redis endpoint address {self.addr!r} is not host:port")
        if host.startswith("["):
            if not host.endswith("]"):
                raise ValueError(f"redis endpoint address {self.addr!r} is malformed")
            host = host[1:-1]
        elif ":" in host:
            raise ValueError(
                f"redis endpoint address {self.addr!r} needs brackets around IPv6"
            )
        if not host or not 0 < int(port) < 1 << 16:
            raise ValueError(f"redis endpoint address {self.addr!r} is not host:port")
        return host, int(port)

    def validate(self) -> None:
        """Raise ValueError for a description that cannot be used safely."""
        self.host_port()
        match self.auth:
            case AuthMode.NONE:
                if self.password:
                    raise ValueError(
                        "redis endpoint has a password but its auth mode is none"
                    )
            case AuthMode.PASSWORD:
                if not self.password:
                    raise ValueError(
                        "redis endpoint auth mode is password but it has none"
                    )
            case AuthMode.IAM:
                if self.password:
                    raise ValueError(
                        "redis endpoint has a password but its auth mode is iam"
                    )
                if not self.tls:
                    raise TokenOverPlaintextError(
                        "redis endpoint sends an IAM token but has no CA certificate "
                        "for TLS"
                    )

    def ssl_context(self) -> ssl.SSLContext | None:
        """The client TLS context, or None for a plaintext endpoint.

        It trusts only the endpoint's CA (not the system store), requires TLS
        1.2 or later, and checks the server certificate's subject alternative
        names against the server name.
        """
        if not self.tls:
            return None
        context = ssl.create_default_context(
            ssl.Purpose.SERVER_AUTH, cadata=self.ca_cert_pem
        )
        context.minimum_version = ssl.TLSVersion.TLSv1_2
        context.check_hostname = True
        context.verify_mode = ssl.CERT_REQUIRED
        return context

    def verified_name(self) -> str:
        """The name the server certificate must carry."""
        return self.server_name or self.host_port()[0]


class _Credentials(Protocol):
    """The part of google.auth credentials the provider uses."""

    @property
    def token(self) -> str | None: ...

    @property
    def expiry(self) -> datetime.datetime | None: ...

    def refresh(self, request: google.auth.transport.Request) -> None: ...


def _utcnow() -> datetime.datetime:
    # google-auth keeps expiry as a naive UTC datetime.
    return datetime.datetime.now(datetime.UTC).replace(tzinfo=None)


class GoogleIAMCredentialProvider(CredentialProvider):
    """Supplies ``default`` and a cached IAM access token to redis-py.

    Only the async client is supported: the sync path would refresh on the
    calling thread.
    """

    def __init__(
        self,
        credentials: _Credentials,
        request: google.auth.transport.Request,
        *,
        clock: Callable[[], datetime.datetime] = _utcnow,
    ) -> None:
        self._credentials = credentials
        self._request = request
        self._clock = clock
        self._lock = anyio.Lock()

    def get_credentials(self) -> tuple[str, str]:
        raise NotImplementedError(
            "GoogleIAMCredentialProvider is async-only; use redis.asyncio"
        )

    async def get_credentials_async(self) -> tuple[str, str]:
        async with self._lock:
            if self._needs_refresh():
                await anyio.to_thread.run_sync(self._credentials.refresh, self._request)
            token = self._credentials.token
        if not token:
            raise RuntimeError("redis iam token is empty after refresh")
        return IAM_USERNAME, token

    def _needs_refresh(self) -> bool:
        if not self._credentials.token:
            return True
        expiry = self._credentials.expiry
        if expiry is None:
            return False
        return expiry - self._clock() <= TOKEN_EARLY_REFRESH


async def default_iam_credential_provider() -> GoogleIAMCredentialProvider:
    """Application Default Credentials for the cloud-platform scope.

    Fetches the first token before returning, so missing credentials fail
    startup rather than the first connection. Finding the credentials can probe
    the metadata server, so it runs in a worker thread too.
    """
    credentials, _ = await anyio.to_thread.run_sync(
        partial(google.auth.default, scopes=[CLOUD_PLATFORM_SCOPE])
    )
    provider = GoogleIAMCredentialProvider(
        credentials, google.auth.transport.requests.Request()
    )
    await provider.get_credentials_async()
    return provider


class VerifiedTLSConnection(Connection):
    """A TCP connection wrapped in TLS with a caller-built context.

    redis-py's own SSL connection always adds the system trust store and
    verifies against the address it dials; this one trusts only the given
    context and verifies the given name, which may differ from the address.
    """

    def __init__(
        self, *, ssl_context: ssl.SSLContext, server_hostname: str, **kwargs: Any
    ) -> None:
        self._ssl_context = ssl_context
        self._server_hostname = server_hostname
        super().__init__(**kwargs)

    def _connection_arguments(self) -> Mapping[str, Any]:
        return {
            **super()._connection_arguments(),
            "ssl": self._ssl_context,
            "server_hostname": self._server_hostname,
        }


# Connection settings the endpoint owns; a caller may not pass them to client().
_ENDPOINT_KWARGS: Final = frozenset(
    {
        "host",
        "port",
        "username",
        "password",
        "credential_provider",
        "connection_class",
        "ssl_context",
        "server_hostname",
        "redis_connect_func",
    }
)


def client(
    endpoint: Endpoint,
    *,
    credential_provider: CredentialProvider | None = None,
    **connection_kwargs: Any,
) -> redis_asyncio.Redis:
    """An async client whose every connection goes to endpoint.

    ``credential_provider`` supplies IAM tokens and is required for an IAM
    endpoint; it is ignored otherwise. ``connection_kwargs`` (timeouts, retry,
    and so on) go to each connection; the endpoint alone decides the address,
    TLS and credentials. Closing the client closes its pool.
    """
    overridden = _ENDPOINT_KWARGS.intersection(connection_kwargs)
    if overridden:
        raise ValueError(
            "redis endpoint decides these connection settings: "
            + ", ".join(sorted(overridden))
        )
    endpoint.validate()
    if endpoint.auth is AuthMode.IAM and credential_provider is None:
        raise ValueError(
            "redis endpoint auth mode is iam but no credential provider was given"
        )

    host, port = endpoint.host_port()
    kwargs: dict[str, Any] = {**connection_kwargs, "host": host, "port": port}
    context = endpoint.ssl_context()
    if context is not None:
        kwargs["connection_class"] = VerifiedTLSConnection
        kwargs["ssl_context"] = context
        kwargs["server_hostname"] = endpoint.verified_name()
    match endpoint.auth:
        case AuthMode.PASSWORD:
            kwargs["password"] = endpoint.password
        case AuthMode.IAM:
            kwargs["credential_provider"] = credential_provider
        case AuthMode.NONE:
            pass

    return redis_asyncio.Redis.from_pool(redis_asyncio.ConnectionPool(**kwargs))
