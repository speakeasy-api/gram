"""Shared test fakes and the event-loop blocking guard for the test suite.

The fakes stand in for the google-cloud-pubsub surfaces the library touches; they
were previously duplicated (and had drifted) between test_backends.py and
test_handle.py, so keeping one copy here is what stops the next divergence.

The aiocop fixtures fail any test whose async handler blocks the event loop —
this library bridges blocking google-cloud-pubsub threads onto an anyio loop, so
a stray synchronous call there would silently saturate it in production. aiocop
patches the *running* asyncio loop, so it only applies to the ``async def`` tests
(pytest-asyncio, asyncio backend); the trio coverage in test_backends.py runs as
sync functions via ``anyio.run`` and is left untouched, which is correct since
aiocop is asyncio-only.
"""

from __future__ import annotations

import threading
from typing import Any

import aiocop
import pytest

# Tasks that run this long without yielding are treated as blocking the loop.
# Matches the threshold the consuming services use (see pystreams.deps.blocking).
DEFAULT_THRESHOLD_MS = 50


@pytest.fixture(scope="session", autouse=True)
def _configure_aiocop():
    """Register the audit hook and slow-task detection once for the session.

    These are loop-independent globals; ``detect_slow_tasks`` itself guards
    against being configured more than once.
    """
    aiocop.patch_audit_functions()
    aiocop.start_blocking_io_detection()
    aiocop.detect_slow_tasks(threshold_ms=DEFAULT_THRESHOLD_MS)
    return


@pytest.fixture(autouse=True)
async def _enforce_no_blocking(_configure_aiocop):
    """Patch this test's running loop and raise on high-severity blocking IO.

    ``activate()`` runs aiocop's on-activate hooks against the loop that is live
    right now (idempotent per loop); ``enable_raise_on_violations`` arms the raise
    for this test's context, so a high-severity blocking call surfaces as a
    ``HighSeverityBlockingIoException`` that fails the test. Async-only: pytest
    skips this fixture for the sync trio tests, which have no asyncio loop to patch.
    """
    aiocop.activate()
    aiocop.enable_raise_on_violations()
    return


class FakeMessage:
    """Duck-typed stand-in for a google-cloud-pubsub Message."""

    def __init__(
        self, data, *, message_id="msg-id", attributes=None, delivery_attempt=None
    ) -> None:
        self.data = data
        self.message_id = message_id
        self.attributes: dict[str, str] = attributes or {}
        self.delivery_attempt = delivery_attempt
        self.acked = False
        self.nacked = False

    def ack(self) -> None:
        self.acked = True

    def nack(self) -> None:
        self.nacked = True


class FakeStreamingFuture:
    """Mimics StreamingPullFuture: ``result`` blocks until the pull ends.

    ``cancel`` resolves it cleanly (the real future resolves with ``True`` once
    the manager finishes shutting down); ``fail`` resolves it with a terminal
    error, the way the library reports a dead subscription.
    """

    def __init__(self) -> None:
        self._done_event = threading.Event()
        self._cancelled = False
        self._exception: BaseException | None = None

    def result(self):
        self._done_event.wait()
        if self._exception is not None:
            raise self._exception
        return True

    def cancel(self) -> None:
        self._cancelled = True
        self._done_event.set()

    def cancelled(self) -> bool:
        return self._cancelled

    def fail(self, exc: BaseException) -> None:
        self._exception = exc
        self._done_event.set()

    def done(self) -> bool:
        return self._done_event.is_set()


class FakeSubscriberClient:
    """Mimics the real client: it schedules messages from a background thread.

    The production google-cloud-pubsub client invokes ``scheduler.schedule`` from
    its own background threads, never from the event-loop thread — so the fake
    must schedule off-thread too, genuinely exercising the scheduler's
    thread-to-loop bridge. The scheduler handed to ``subscribe`` is captured on
    ``self.scheduler`` so tests can drive library-side flows (e.g. the manager's
    shutdown thread calling ``scheduler.shutdown``).
    """

    def __init__(self, messages) -> None:
        self._messages = messages
        self.future = FakeStreamingFuture()
        # The _PortalScheduler handed to subscribe(); Any because tests poke
        # library-side entry points (schedule/shutdown) on it directly.
        self.scheduler: Any = None

    def subscribe(self, subscription_path, callback, *, scheduler, **kwargs):
        self.scheduler = scheduler

        def pump() -> None:
            for message in self._messages:
                scheduler.schedule(callback, message)

        threading.Thread(target=pump, daemon=True).start()
        return self.future
