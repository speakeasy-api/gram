"""Shared test fakes for the google-cloud-pubsub surfaces the library touches.

These were previously duplicated (and had drifted) between test_backends.py and
test_handle.py; keeping one copy here is what stops the next divergence.
"""

from __future__ import annotations

import threading
from typing import Any


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
