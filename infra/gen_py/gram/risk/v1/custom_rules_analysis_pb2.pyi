from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class CustomRulesAnalysis(_message.Message):
    __slots__ = ("request_id", "chat_message_id", "project_id", "organization_id", "risk_policy_id", "risk_policy_version", "created_at", "content", "kind", "tool_calls", "custom_rule_ids")
    class ToolCall(_message.Message):
        __slots__ = ("name", "arguments")
        NAME_FIELD_NUMBER: _ClassVar[int]
        ARGUMENTS_FIELD_NUMBER: _ClassVar[int]
        name: str
        arguments: str
        def __init__(self, name: _Optional[str] = ..., arguments: _Optional[str] = ...) -> None: ...
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    CHAT_MESSAGE_ID_FIELD_NUMBER: _ClassVar[int]
    PROJECT_ID_FIELD_NUMBER: _ClassVar[int]
    ORGANIZATION_ID_FIELD_NUMBER: _ClassVar[int]
    RISK_POLICY_ID_FIELD_NUMBER: _ClassVar[int]
    RISK_POLICY_VERSION_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    CONTENT_FIELD_NUMBER: _ClassVar[int]
    KIND_FIELD_NUMBER: _ClassVar[int]
    TOOL_CALLS_FIELD_NUMBER: _ClassVar[int]
    CUSTOM_RULE_IDS_FIELD_NUMBER: _ClassVar[int]
    request_id: str
    chat_message_id: str
    project_id: str
    organization_id: str
    risk_policy_id: str
    risk_policy_version: int
    created_at: str
    content: str
    kind: str
    tool_calls: _containers.RepeatedCompositeFieldContainer[CustomRulesAnalysis.ToolCall]
    custom_rule_ids: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, request_id: _Optional[str] = ..., chat_message_id: _Optional[str] = ..., project_id: _Optional[str] = ..., organization_id: _Optional[str] = ..., risk_policy_id: _Optional[str] = ..., risk_policy_version: _Optional[int] = ..., created_at: _Optional[str] = ..., content: _Optional[str] = ..., kind: _Optional[str] = ..., tool_calls: _Optional[_Iterable[_Union[CustomRulesAnalysis.ToolCall, _Mapping]]] = ..., custom_rule_ids: _Optional[_Iterable[str]] = ...) -> None: ...
