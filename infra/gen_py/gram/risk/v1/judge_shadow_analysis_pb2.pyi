from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Optional as _Optional

DESCRIPTOR: _descriptor.FileDescriptor

class JudgeShadowAnalysis(_message.Message):
    __slots__ = ("comparison_id", "organization_id", "project_id", "detector", "state_json", "baseline_outcome", "baseline_model", "baseline_duration_seconds", "baseline_trace_id", "policy_hash", "created_at")
    COMPARISON_ID_FIELD_NUMBER: _ClassVar[int]
    ORGANIZATION_ID_FIELD_NUMBER: _ClassVar[int]
    PROJECT_ID_FIELD_NUMBER: _ClassVar[int]
    DETECTOR_FIELD_NUMBER: _ClassVar[int]
    STATE_JSON_FIELD_NUMBER: _ClassVar[int]
    BASELINE_OUTCOME_FIELD_NUMBER: _ClassVar[int]
    BASELINE_MODEL_FIELD_NUMBER: _ClassVar[int]
    BASELINE_DURATION_SECONDS_FIELD_NUMBER: _ClassVar[int]
    BASELINE_TRACE_ID_FIELD_NUMBER: _ClassVar[int]
    POLICY_HASH_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    comparison_id: str
    organization_id: str
    project_id: str
    detector: str
    state_json: bytes
    baseline_outcome: str
    baseline_model: str
    baseline_duration_seconds: float
    baseline_trace_id: str
    policy_hash: str
    created_at: str
    def __init__(self, comparison_id: _Optional[str] = ..., organization_id: _Optional[str] = ..., project_id: _Optional[str] = ..., detector: _Optional[str] = ..., state_json: _Optional[bytes] = ..., baseline_outcome: _Optional[str] = ..., baseline_model: _Optional[str] = ..., baseline_duration_seconds: _Optional[float] = ..., baseline_trace_id: _Optional[str] = ..., policy_hash: _Optional[str] = ..., created_at: _Optional[str] = ...) -> None: ...
