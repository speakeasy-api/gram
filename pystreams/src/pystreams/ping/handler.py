import logging

import structlog
from gram.ping.v2 import ping_pb2
from gram_infra.pubsub.subscriber import MessageMetadata


class PingHandler:
    def __init__(
        self,
        logger: structlog.stdlib.BoundLogger,
        log_level: int = logging.DEBUG,
    ):
        self.logger = logger
        self.log_level = log_level

    async def handle(self, message: ping_pb2.Message, meta: MessageMetadata) -> None:
        # Returning acks the message; raising nacks it (triggering redelivery
        # and eventual dead-lettering per PyProcessor's dead_letter policy).
        self.logger.log(
            self.log_level,
            "received message",
            message_id=message.id,
            message_type=message.type,
            payload=message.payload.decode("utf-8", "replace"),
            delivery_attempt=meta.delivery_attempt,
        )
