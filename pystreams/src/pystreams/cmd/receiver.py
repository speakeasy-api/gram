"""Subscription receiver wiring for the streams command.

Bundles subscriber construction, the per-message tracing wrapper, and task
spawning behind a single :meth:`ReceiverGroup.receive` call — mirroring the Go
``receiverGroup`` in ``server/cmd/gram/streams.go`` — so each subscription is
registered in one line and its topic/subscription proto names are derived once,
not repeated at the call site.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import TypeVar

import anyio.abc
import structlog
from google.protobuf.message import Message

from gram_infra.pubsub import SubscriberBroker, pubsub_subscriber_for_message
from gram_infra.pubsub.subscriber import MessageCallback

from ..deps.tracing import traced

M = TypeVar("M", bound=Message)


@dataclass
class ReceiverGroup:
    """Shared dependencies for registering subscription receivers."""

    task_group: anyio.abc.TaskGroup
    broker: SubscriberBroker
    logger: structlog.stdlib.BoundLogger

    def receive(
        self,
        message_type: type[M],
        subscription_type: type[Message],
        handler: MessageCallback[M],
    ) -> None:
        """Resolve a subscriber for ``message_type`` and start consuming it.

        The handler is wrapped so every delivered message runs inside a
        ``stream.handleMessage`` span tagged with the topic and subscription
        proto names.
        """
        subscriber = pubsub_subscriber_for_message(
            self.broker,
            message_type,
            subscription_type,
            logger=self.logger,
        )
        self.task_group.start_soon(
            subscriber.receive,
            traced(
                handler,
                topic_proto_name=message_type.DESCRIPTOR.full_name,
                subscription_proto_name=subscription_type.DESCRIPTOR.full_name,
            ),
        )
