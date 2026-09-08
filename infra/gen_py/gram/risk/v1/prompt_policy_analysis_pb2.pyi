from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class PromptPolicyAnalysis(_message.Message):
    __slots__ = ("request_id", "chat_message_id", "project_id", "organization_id", "risk_policy_id", "risk_policy_version", "created_at", "content", "user_id", "prompt", "model_config", "message_type", "body", "tool_name", "tool_calls", "content_part_id", "chat_id", "parent_chat_message_id", "origin_risk_policy_id", "origin_risk_policy_version", "message_link_reason", "execution_path", "tool_call_id", "hook_source")
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
    USER_ID_FIELD_NUMBER: _ClassVar[int]
    PROMPT_FIELD_NUMBER: _ClassVar[int]
    MODEL_CONFIG_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_TYPE_FIELD_NUMBER: _ClassVar[int]
    BODY_FIELD_NUMBER: _ClassVar[int]
    TOOL_NAME_FIELD_NUMBER: _ClassVar[int]
    TOOL_CALLS_FIELD_NUMBER: _ClassVar[int]
    CONTENT_PART_ID_FIELD_NUMBER: _ClassVar[int]
    CHAT_ID_FIELD_NUMBER: _ClassVar[int]
    PARENT_CHAT_MESSAGE_ID_FIELD_NUMBER: _ClassVar[int]
    ORIGIN_RISK_POLICY_ID_FIELD_NUMBER: _ClassVar[int]
    ORIGIN_RISK_POLICY_VERSION_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_LINK_REASON_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_PATH_FIELD_NUMBER: _ClassVar[int]
    TOOL_CALL_ID_FIELD_NUMBER: _ClassVar[int]
    HOOK_SOURCE_FIELD_NUMBER: _ClassVar[int]
    request_id: str
    chat_message_id: str
    project_id: str
    organization_id: str
    risk_policy_id: str
    risk_policy_version: int
    created_at: str
    content: str
    user_id: str
    prompt: str
    model_config: bytes
    message_type: str
    body: str
    tool_name: str
    tool_calls: _containers.RepeatedCompositeFieldContainer[PromptPolicyAnalysis.ToolCall]
    content_part_id: str
    chat_id: str
    parent_chat_message_id: str
    origin_risk_policy_id: str
    origin_risk_policy_version: int
    message_link_reason: str
    execution_path: str
    tool_call_id: str
    hook_source: str
    def __init__(self, request_id: _Optional[str] = ..., chat_message_id: _Optional[str] = ..., project_id: _Optional[str] = ..., organization_id: _Optional[str] = ..., risk_policy_id: _Optional[str] = ..., risk_policy_version: _Optional[int] = ..., created_at: _Optional[str] = ..., content: _Optional[str] = ..., user_id: _Optional[str] = ..., prompt: _Optional[str] = ..., model_config: _Optional[bytes] = ..., message_type: _Optional[str] = ..., body: _Optional[str] = ..., tool_name: _Optional[str] = ..., tool_calls: _Optional[_Iterable[_Union[PromptPolicyAnalysis.ToolCall, _Mapping]]] = ..., content_part_id: _Optional[str] = ..., chat_id: _Optional[str] = ..., parent_chat_message_id: _Optional[str] = ..., origin_risk_policy_id: _Optional[str] = ..., origin_risk_policy_version: _Optional[int] = ..., message_link_reason: _Optional[str] = ..., execution_path: _Optional[str] = ..., tool_call_id: _Optional[str] = ..., hook_source: _Optional[str] = ...) -> None: ...
