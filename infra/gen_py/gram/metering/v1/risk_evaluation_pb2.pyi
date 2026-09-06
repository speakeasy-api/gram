from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class RiskEvaluation(_message.Message):
    __slots__ = ("id", "evaluation_id", "organization_id", "project_id", "policy_id", "policy_version", "operation_id", "detector", "execution_mode", "outcome", "occurred_at", "produced_at", "stokens", "measurement_method", "model", "provider_request_id", "prompt_tokens", "completion_tokens", "cost_usd", "measurement_error", "record_kind")
    class Detector(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
        __slots__ = ()
        DETECTOR_UNSPECIFIED: _ClassVar[RiskEvaluation.Detector]
        DETECTOR_GITLEAKS: _ClassVar[RiskEvaluation.Detector]
        DETECTOR_PRESIDIO: _ClassVar[RiskEvaluation.Detector]
        DETECTOR_PROMPT_INJECTION: _ClassVar[RiskEvaluation.Detector]
        DETECTOR_PROMPT_POLICY: _ClassVar[RiskEvaluation.Detector]
        DETECTOR_CUSTOM_RULES: _ClassVar[RiskEvaluation.Detector]
    DETECTOR_UNSPECIFIED: RiskEvaluation.Detector
    DETECTOR_GITLEAKS: RiskEvaluation.Detector
    DETECTOR_PRESIDIO: RiskEvaluation.Detector
    DETECTOR_PROMPT_INJECTION: RiskEvaluation.Detector
    DETECTOR_PROMPT_POLICY: RiskEvaluation.Detector
    DETECTOR_CUSTOM_RULES: RiskEvaluation.Detector
    class ExecutionMode(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
        __slots__ = ()
        EXECUTION_MODE_UNSPECIFIED: _ClassVar[RiskEvaluation.ExecutionMode]
        EXECUTION_MODE_REALTIME: _ClassVar[RiskEvaluation.ExecutionMode]
        EXECUTION_MODE_BATCH: _ClassVar[RiskEvaluation.ExecutionMode]
        EXECUTION_MODE_SHADOW: _ClassVar[RiskEvaluation.ExecutionMode]
    EXECUTION_MODE_UNSPECIFIED: RiskEvaluation.ExecutionMode
    EXECUTION_MODE_REALTIME: RiskEvaluation.ExecutionMode
    EXECUTION_MODE_BATCH: RiskEvaluation.ExecutionMode
    EXECUTION_MODE_SHADOW: RiskEvaluation.ExecutionMode
    class Outcome(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
        __slots__ = ()
        OUTCOME_UNSPECIFIED: _ClassVar[RiskEvaluation.Outcome]
        OUTCOME_COMPLETED: _ClassVar[RiskEvaluation.Outcome]
        OUTCOME_FAILED: _ClassVar[RiskEvaluation.Outcome]
        OUTCOME_CANCELLED: _ClassVar[RiskEvaluation.Outcome]
        OUTCOME_SKIPPED: _ClassVar[RiskEvaluation.Outcome]
    OUTCOME_UNSPECIFIED: RiskEvaluation.Outcome
    OUTCOME_COMPLETED: RiskEvaluation.Outcome
    OUTCOME_FAILED: RiskEvaluation.Outcome
    OUTCOME_CANCELLED: RiskEvaluation.Outcome
    OUTCOME_SKIPPED: RiskEvaluation.Outcome
    ID_FIELD_NUMBER: _ClassVar[int]
    EVALUATION_ID_FIELD_NUMBER: _ClassVar[int]
    ORGANIZATION_ID_FIELD_NUMBER: _ClassVar[int]
    PROJECT_ID_FIELD_NUMBER: _ClassVar[int]
    POLICY_ID_FIELD_NUMBER: _ClassVar[int]
    POLICY_VERSION_FIELD_NUMBER: _ClassVar[int]
    OPERATION_ID_FIELD_NUMBER: _ClassVar[int]
    DETECTOR_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_MODE_FIELD_NUMBER: _ClassVar[int]
    OUTCOME_FIELD_NUMBER: _ClassVar[int]
    OCCURRED_AT_FIELD_NUMBER: _ClassVar[int]
    PRODUCED_AT_FIELD_NUMBER: _ClassVar[int]
    STOKENS_FIELD_NUMBER: _ClassVar[int]
    MEASUREMENT_METHOD_FIELD_NUMBER: _ClassVar[int]
    MODEL_FIELD_NUMBER: _ClassVar[int]
    PROVIDER_REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    PROMPT_TOKENS_FIELD_NUMBER: _ClassVar[int]
    COMPLETION_TOKENS_FIELD_NUMBER: _ClassVar[int]
    COST_USD_FIELD_NUMBER: _ClassVar[int]
    MEASUREMENT_ERROR_FIELD_NUMBER: _ClassVar[int]
    RECORD_KIND_FIELD_NUMBER: _ClassVar[int]
    id: str
    evaluation_id: str
    organization_id: str
    project_id: str
    policy_id: str
    policy_version: int
    operation_id: str
    detector: RiskEvaluation.Detector
    execution_mode: RiskEvaluation.ExecutionMode
    outcome: RiskEvaluation.Outcome
    occurred_at: str
    produced_at: str
    stokens: int
    measurement_method: str
    model: str
    provider_request_id: str
    prompt_tokens: int
    completion_tokens: int
    cost_usd: float
    measurement_error: bool
    record_kind: str
    def __init__(self, id: _Optional[str] = ..., evaluation_id: _Optional[str] = ..., organization_id: _Optional[str] = ..., project_id: _Optional[str] = ..., policy_id: _Optional[str] = ..., policy_version: _Optional[int] = ..., operation_id: _Optional[str] = ..., detector: _Optional[_Union[RiskEvaluation.Detector, str]] = ..., execution_mode: _Optional[_Union[RiskEvaluation.ExecutionMode, str]] = ..., outcome: _Optional[_Union[RiskEvaluation.Outcome, str]] = ..., occurred_at: _Optional[str] = ..., produced_at: _Optional[str] = ..., stokens: _Optional[int] = ..., measurement_method: _Optional[str] = ..., model: _Optional[str] = ..., provider_request_id: _Optional[str] = ..., prompt_tokens: _Optional[int] = ..., completion_tokens: _Optional[int] = ..., cost_usd: _Optional[float] = ..., measurement_error: _Optional[bool] = ..., record_kind: _Optional[str] = ...) -> None: ...
