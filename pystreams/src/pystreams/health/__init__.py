"""HTTP server for Kubernetes health probes, built on Starlette + uvicorn.

Kubernetes liveness/readiness probes are plain ``GET`` requests that only care
about the status code. Two endpoints are exposed:

- ``GET /healthz`` (liveness): always ``200``. The fact that the event loop
  served the request is itself the liveness signal — if the loop were wedged,
  the probe would time out. Kubernetes restarts the pod when this fails.
- ``GET /readyz`` (readiness): ``200`` once :class:`HealthState` is marked ready
  (the subscriber's receive loop is running), ``503`` otherwise. The worker
  flips it off while shutting down, so Kubernetes stops considering the pod
  ready and a rolling deploy only advances once the new pod is consuming.

uvicorn is driven programmatically so it runs as a task inside the worker's own
anyio task group rather than taking over the process. Its signal handlers are
disabled because the worker already owns SIGINT/SIGTERM (see ``cmd.multi``).
"""

from __future__ import annotations

import anyio
import structlog
import uvicorn
from anyio.abc import TaskStatus
from starlette.applications import Starlette
from starlette.requests import Request
from starlette.responses import PlainTextResponse
from starlette.routing import Route

__all__ = ["LIVENESS_PATH", "READINESS_PATH", "HealthState", "serve_control"]

LIVENESS_PATH = "/healthz"
READINESS_PATH = "/readyz"


class HealthState:
    """Readiness flag shared between the worker and the health server.

    Liveness is implicit (a live event loop answers the probe), so only
    readiness needs tracking. The worker calls :meth:`set_ready` once the
    subscriber is consuming and :meth:`set_not_ready` while tearing down. The
    flag is a plain bool touched only on the event-loop thread, so no lock is
    needed.
    """

    def __init__(self) -> None:
        self._ready = False

    @property
    def ready(self) -> bool:
        return self._ready

    def set_ready(self) -> None:
        self._ready = True

    def set_not_ready(self) -> None:
        self._ready = False


class _NoSignalServer(uvicorn.Server):
    """uvicorn server that leaves signal handling to the worker.

    The worker installs its own anyio SIGINT/SIGTERM receiver; letting uvicorn
    also grab them would mean two competing handlers. This is cancelled along
    with the worker's task group on shutdown.
    """

    def install_signal_handlers(self) -> None:
        return None


def _build_app(state: HealthState) -> Starlette:
    async def healthz(_request: Request) -> PlainTextResponse:
        return PlainTextResponse("ok")

    async def readyz(_request: Request) -> PlainTextResponse:
        if state.ready:
            return PlainTextResponse("ready")
        return PlainTextResponse("not ready", status_code=503)

    return Starlette(
        routes=[
            Route(LIVENESS_PATH, healthz, methods=["GET"]),
            Route(READINESS_PATH, readyz, methods=["GET"]),
        ]
    )


async def serve_control(
    state: HealthState,
    *,
    host: str,
    port: int,
    logger: structlog.stdlib.BoundLogger,
    task_status: TaskStatus[int] = anyio.TASK_STATUS_IGNORED,
) -> None:
    """Serve control endpoints until cancelled.

    Designed to be launched with ``task_group.start`` so binding completes
    before the caller proceeds: a bind failure surfaces immediately, and the
    actual bound port is reported back through ``task_status`` (so ``port=0``
    yields an OS-assigned port, which the tests rely on).
    """
    config = uvicorn.Config(
        _build_app(state),
        host=host,
        port=port,
        # The worker configures structlog itself; keep uvicorn from installing
        # its own stdlib logging config or emitting a line per probe request.
        log_config=None,
        access_log=False,
        lifespan="off",
    )
    server = _NoSignalServer(config)

    async with anyio.create_task_group() as tg:
        tg.start_soon(server.serve)

        # uvicorn flips ``started`` once the listening sockets are bound. It
        # exposes only this polled boolean, not an awaitable event, so a poll
        # loop is the available option here.
        while not server.started:  # noqa: ASYNC110
            await anyio.sleep(0.02)

        bound_port = _bound_port(server, fallback=port)
        logger.info("control server listening", host=host, port=bound_port)
        task_status.started(bound_port)


def _bound_port(server: uvicorn.Server, *, fallback: int) -> int:
    """Best-effort read of the port uvicorn actually bound to."""
    try:
        for started_server in server.servers:
            for sock in started_server.sockets:
                return sock.getsockname()[1]
    except Exception:
        pass
    return fallback
