from functools import partial

import anyio
import structlog

from pystreams.health import HealthState, serve_control


async def _get(port: int, path: str) -> bytes:
    """Issue a bare HTTP/1.1 GET and return the full response bytes.

    ``Connection: close`` asks the server to close after responding, so reading
    until EndOfStream yields exactly one complete response (uvicorn keeps
    connections alive otherwise).
    """
    stream = await anyio.connect_tcp("127.0.0.1", port)
    async with stream:
        await stream.send(
            f"GET {path} HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n".encode()
        )
        data = b""
        try:
            while True:
                data += await stream.receive()
        except anyio.EndOfStream:
            pass
    return data


def _status_line(response: bytes) -> str:
    return response.split(b"\r\n", 1)[0].decode("latin-1")


async def test_health_endpoints_reflect_readiness():
    state = HealthState()
    logger = structlog.get_logger()

    async with anyio.create_task_group() as tg:
        port = await tg.start(
            partial(serve_control, state, host="127.0.0.1", port=0, logger=logger)
        )

        # Liveness is up immediately and independent of readiness.
        assert "200 OK" in _status_line(await _get(port, "/healthz"))

        # Not ready yet -> 503.
        assert "503" in _status_line(await _get(port, "/readyz"))

        state.set_ready()
        assert "200 OK" in _status_line(await _get(port, "/readyz"))

        # Flipping back to not-ready (e.g. during shutdown) returns 503 again.
        state.set_not_ready()
        assert "503" in _status_line(await _get(port, "/readyz"))

        # Liveness keeps answering regardless of readiness.
        assert "200 OK" in _status_line(await _get(port, "/healthz"))

        # Unknown paths 404.
        assert "404" in _status_line(await _get(port, "/nope"))

        tg.cancel_scope.cancel()
