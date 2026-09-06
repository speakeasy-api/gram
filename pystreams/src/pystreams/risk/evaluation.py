from __future__ import annotations

import hashlib
import uuid
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Final, Protocol

import anyio
import structlog
import tiktoken
from anyio import to_thread
from gram.metering.v1.risk_evaluation_pb2 import RiskEvaluation
from gram_infra.pubsub import PublishResult

MEASUREMENT_METHOD: Final = "tiktoken_o200k_base"

_PUBLISH_TIMEOUT_SECONDS: Final = 10.0
_EVALUATION_ID_PREFIX: Final = "gram:risk:evaluation:v1"
_OPERATION_ID_PREFIX: Final = b"gram:risk:operation:v1\x00"
_NO_SPECIAL_TOKENS: Final = frozenset[str]()

# These labels are part of the logical identity, independent of enum names.
_DETECTOR_IDENTITY_LABELS: Final = {
    RiskEvaluation.DETECTOR_GITLEAKS: "gitleaks",
    RiskEvaluation.DETECTOR_PRESIDIO: "presidio",
    RiskEvaluation.DETECTOR_PROMPT_INJECTION: "prompt_injection",
    RiskEvaluation.DETECTOR_PROMPT_POLICY: "prompt_policy",
    RiskEvaluation.DETECTOR_CUSTOM_RULES: "custom_rules",
}
_MODE_IDENTITY_LABELS: Final = {
    RiskEvaluation.EXECUTION_MODE_REALTIME: "realtime",
    RiskEvaluation.EXECUTION_MODE_BATCH: "batch",
    RiskEvaluation.EXECUTION_MODE_SHADOW: "shadow",
}


class RiskEvaluationPublisher(Protocol):
    def publish(self, message: RiskEvaluation) -> PublishResult: ...


@dataclass(frozen=True)
class Evaluation:
    organization_id: str
    project_id: str
    operation_id: str
    detector: RiskEvaluation.Detector
    execution_mode: RiskEvaluation.ExecutionMode
    policy_id: str = ""
    policy_version: int = 0


async def build_risk_evaluation_recorder(
    logger: structlog.stdlib.BoundLogger,
    publisher: RiskEvaluationPublisher,
) -> RiskEvaluationRecorder:
    """Initialize the canonical tokenizer off-loop before steady-state detection."""

    encoding = await to_thread.run_sync(tiktoken.get_encoding, "o200k_base")
    return RiskEvaluationRecorder(logger, publisher, encoding=encoding)


class RiskEvaluationRecorder:
    """Records safe terminal detector attempts without retaining scanned text."""

    def __init__(
        self,
        logger: structlog.stdlib.BoundLogger,
        publisher: RiskEvaluationPublisher,
        *,
        encoding: tiktoken.Encoding,
    ) -> None:
        self._logger = logger
        self._publisher = publisher
        self._encoding = encoding

    def start(self, evaluation: Evaluation) -> EvaluationAttempt:
        return EvaluationAttempt(
            logger=self._logger,
            publisher=self._publisher,
            encoding=self._encoding,
            evaluation=evaluation,
            occurred_at=datetime.now(UTC),
        )


class EvaluationAttempt:
    def __init__(
        self,
        *,
        logger: structlog.stdlib.BoundLogger,
        publisher: RiskEvaluationPublisher,
        encoding: tiktoken.Encoding,
        evaluation: Evaluation,
        occurred_at: datetime,
    ) -> None:
        self._logger = logger
        self._publisher = publisher
        self._encoding = encoding
        self._evaluation = evaluation
        self._occurred_at = occurred_at
        self._stokens: int | None = None
        self._measurement_error = False
        self._finished = False

    async def measure(self, fragments: list[str]) -> None:
        """Count fragments separately off-loop; tokenization failure stays unknown."""

        try:
            self._stokens = await to_thread.run_sync(self._count_fragments, fragments)
        except Exception as exc:
            self._stokens = None
            self._measurement_error = True
            self._logger.warning(
                "risk evaluation tokenization failed",
                evaluation_id=evaluation_id(self._evaluation),
                detector=self._evaluation.detector,
                execution_mode=self._evaluation.execution_mode,
                error_type=type(exc).__name__,
            )

    def _count_fragments(self, fragments: list[str]) -> int:
        # Treat every fragment as ordinary text. In particular, strings that
        # resemble tiktoken special tokens must be encoded rather than rejected
        # or interpreted as control tokens.
        return sum(
            len(
                self._encoding.encode(
                    fragment,
                    allowed_special=_NO_SPECIAL_TOKENS,
                    disallowed_special=(),
                )
            )
            for fragment in fragments
        )

    async def finish(
        self, outcome: RiskEvaluation.Outcome, *, include_volume: bool = True
    ) -> None:
        if self._finished:
            raise RuntimeError("risk evaluation attempt already finished")
        self._finished = True

        event = RiskEvaluation(
            id=str(uuid.uuid7()),
            evaluation_id=evaluation_id(self._evaluation),
            organization_id=self._evaluation.organization_id,
            project_id=self._evaluation.project_id,
            operation_id=self._evaluation.operation_id,
            detector=self._evaluation.detector,
            execution_mode=self._evaluation.execution_mode,
            outcome=outcome,
            occurred_at=_rfc3339(self._occurred_at),
            produced_at=_rfc3339(datetime.now(UTC)),
            measurement_method=MEASUREMENT_METHOD,
            policy_id=self._evaluation.policy_id,
            policy_version=self._evaluation.policy_version,
            record_kind="scan",
        )
        if include_volume and self._stokens is not None:
            event.stokens = self._stokens
        if include_volume and self._measurement_error:
            event.measurement_error = True
        try:
            # Completion publication must not change the scan's existing
            # ack/nack or enforcement behavior, including during shutdown.
            with anyio.move_on_after(
                _PUBLISH_TIMEOUT_SECONDS, shield=True
            ) as publish_scope:
                await self._publisher.publish(event).get()
            if publish_scope.cancel_called:
                self._log_publish_failure(event, "TimeoutError")
        except Exception as exc:
            self._log_publish_failure(event, type(exc).__name__)

    def _log_publish_failure(self, event: RiskEvaluation, error_type: str) -> None:
        self._logger.error(
            "publish risk evaluation failed",
            evaluation_id=event.evaluation_id,
            detector=event.detector,
            execution_mode=event.execution_mode,
            outcome=event.outcome,
            error_type=error_type,
        )


def evaluation_id(evaluation: Evaluation) -> str:
    name = "\x00".join(
        (
            _EVALUATION_ID_PREFIX,
            evaluation.organization_id,
            evaluation.project_id,
            _MODE_IDENTITY_LABELS[evaluation.execution_mode],
            _DETECTOR_IDENTITY_LABELS[evaluation.detector],
            evaluation.operation_id,
        )
    )
    return str(uuid.uuid5(uuid.NAMESPACE_URL, name))


def operation_id(*parts: str) -> str:
    """Return a safe deterministic id from unambiguous length-framed parts."""

    digest = hashlib.sha256()
    digest.update(_OPERATION_ID_PREFIX)
    for part in parts:
        encoded = part.encode()
        digest.update(len(encoded).to_bytes(8, "big"))
        digest.update(encoded)
    return "riskop:v1:" + digest.hexdigest()


def shadow_operation_id(
    *,
    request_id: str,
    chat_message_id: str,
    content_part_id: str,
    policy_id: str,
    policy_version: int,
) -> str:
    if chat_message_id or content_part_id:
        return operation_id(
            "content",
            chat_message_id,
            content_part_id,
            policy_id,
            str(policy_version),
        )
    return operation_id("request", request_id, policy_id, str(policy_version))


def realtime_operation_id(request_id: str) -> str:
    # Realtime Presidio is shared detector work. Policy attribution remains on
    # the event but must not multiply the logical identity.
    return operation_id("request", request_id)


def _rfc3339(value: datetime) -> str:
    return (
        value.astimezone(UTC).isoformat(timespec="microseconds").replace("+00:00", "Z")
    )
