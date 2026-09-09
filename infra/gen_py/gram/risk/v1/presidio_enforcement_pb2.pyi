from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable
from typing import ClassVar as _ClassVar, Optional as _Optional

DESCRIPTOR: _descriptor.FileDescriptor

class PresidioEnforcement(_message.Message):
    __slots__ = ("request_id", "chat_message_id", "project_id", "organization_id", "risk_policy_id", "risk_policy_version", "created_at", "content", "content_part_id", "entities", "score_threshold", "chat_id", "parent_chat_message_id", "origin_risk_policy_id", "origin_risk_policy_version", "message_link_reason", "execution_path", "tool_call_id", "tool_name", "hook_source", "user_id", "message_type", "meter_reading", "policy_link_reason")
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    CHAT_MESSAGE_ID_FIELD_NUMBER: _ClassVar[int]
    PROJECT_ID_FIELD_NUMBER: _ClassVar[int]
    ORGANIZATION_ID_FIELD_NUMBER: _ClassVar[int]
    RISK_POLICY_ID_FIELD_NUMBER: _ClassVar[int]
    RISK_POLICY_VERSION_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    CONTENT_FIELD_NUMBER: _ClassVar[int]
    CONTENT_PART_ID_FIELD_NUMBER: _ClassVar[int]
    ENTITIES_FIELD_NUMBER: _ClassVar[int]
    SCORE_THRESHOLD_FIELD_NUMBER: _ClassVar[int]
    CHAT_ID_FIELD_NUMBER: _ClassVar[int]
    PARENT_CHAT_MESSAGE_ID_FIELD_NUMBER: _ClassVar[int]
    ORIGIN_RISK_POLICY_ID_FIELD_NUMBER: _ClassVar[int]
    ORIGIN_RISK_POLICY_VERSION_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_LINK_REASON_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_PATH_FIELD_NUMBER: _ClassVar[int]
    TOOL_CALL_ID_FIELD_NUMBER: _ClassVar[int]
    TOOL_NAME_FIELD_NUMBER: _ClassVar[int]
    HOOK_SOURCE_FIELD_NUMBER: _ClassVar[int]
    USER_ID_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_TYPE_FIELD_NUMBER: _ClassVar[int]
    METER_READING_FIELD_NUMBER: _ClassVar[int]
    POLICY_LINK_REASON_FIELD_NUMBER: _ClassVar[int]
    request_id: str
    chat_message_id: str
    project_id: str
    organization_id: str
    risk_policy_id: str
    risk_policy_version: int
    created_at: str
    content: str
    content_part_id: str
    entities: _containers.RepeatedScalarFieldContainer[str]
    score_threshold: float
    chat_id: str
    parent_chat_message_id: str
    origin_risk_policy_id: str
    origin_risk_policy_version: int
    message_link_reason: str
    execution_path: str
    tool_call_id: str
    tool_name: str
    hook_source: str
    user_id: str
    message_type: str
    meter_reading: bytes
    policy_link_reason: str
    def __init__(self, request_id: _Optional[str] = ..., chat_message_id: _Optional[str] = ..., project_id: _Optional[str] = ..., organization_id: _Optional[str] = ..., risk_policy_id: _Optional[str] = ..., risk_policy_version: _Optional[int] = ..., created_at: _Optional[str] = ..., content: _Optional[str] = ..., content_part_id: _Optional[str] = ..., entities: _Optional[_Iterable[str]] = ..., score_threshold: _Optional[float] = ..., chat_id: _Optional[str] = ..., parent_chat_message_id: _Optional[str] = ..., origin_risk_policy_id: _Optional[str] = ..., origin_risk_policy_version: _Optional[int] = ..., message_link_reason: _Optional[str] = ..., execution_path: _Optional[str] = ..., tool_call_id: _Optional[str] = ..., tool_name: _Optional[str] = ..., hook_source: _Optional[str] = ..., user_id: _Optional[str] = ..., message_type: _Optional[str] = ..., meter_reading: _Optional[bytes] = ..., policy_link_reason: _Optional[str] = ...) -> None: ...
