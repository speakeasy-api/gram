from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class TranscriptMessage(_message.Message):
    __slots__ = ("id", "analysis_id", "organization_id", "project_id", "actor", "chat_id", "generation", "message_id", "sequence", "role", "content", "content_bytes", "content_truncated", "tool_calls", "tool_call_id", "model", "redacted", "withheld", "created_at", "exported_at")
    class Actor(_message.Message):
        __slots__ = ("user_id", "user_email")
        USER_ID_FIELD_NUMBER: _ClassVar[int]
        USER_EMAIL_FIELD_NUMBER: _ClassVar[int]
        user_id: str
        user_email: str
        def __init__(self, user_id: _Optional[str] = ..., user_email: _Optional[str] = ...) -> None: ...
    class ToolCall(_message.Message):
        __slots__ = ("tool_call_id", "tool_name", "mcp_server", "mcp_function", "arguments", "arguments_bytes", "arguments_truncated")
        TOOL_CALL_ID_FIELD_NUMBER: _ClassVar[int]
        TOOL_NAME_FIELD_NUMBER: _ClassVar[int]
        MCP_SERVER_FIELD_NUMBER: _ClassVar[int]
        MCP_FUNCTION_FIELD_NUMBER: _ClassVar[int]
        ARGUMENTS_FIELD_NUMBER: _ClassVar[int]
        ARGUMENTS_BYTES_FIELD_NUMBER: _ClassVar[int]
        ARGUMENTS_TRUNCATED_FIELD_NUMBER: _ClassVar[int]
        tool_call_id: str
        tool_name: str
        mcp_server: str
        mcp_function: str
        arguments: str
        arguments_bytes: int
        arguments_truncated: bool
        def __init__(self, tool_call_id: _Optional[str] = ..., tool_name: _Optional[str] = ..., mcp_server: _Optional[str] = ..., mcp_function: _Optional[str] = ..., arguments: _Optional[str] = ..., arguments_bytes: _Optional[int] = ..., arguments_truncated: _Optional[bool] = ...) -> None: ...
    ID_FIELD_NUMBER: _ClassVar[int]
    ANALYSIS_ID_FIELD_NUMBER: _ClassVar[int]
    ORGANIZATION_ID_FIELD_NUMBER: _ClassVar[int]
    PROJECT_ID_FIELD_NUMBER: _ClassVar[int]
    ACTOR_FIELD_NUMBER: _ClassVar[int]
    CHAT_ID_FIELD_NUMBER: _ClassVar[int]
    GENERATION_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_ID_FIELD_NUMBER: _ClassVar[int]
    SEQUENCE_FIELD_NUMBER: _ClassVar[int]
    ROLE_FIELD_NUMBER: _ClassVar[int]
    CONTENT_FIELD_NUMBER: _ClassVar[int]
    CONTENT_BYTES_FIELD_NUMBER: _ClassVar[int]
    CONTENT_TRUNCATED_FIELD_NUMBER: _ClassVar[int]
    TOOL_CALLS_FIELD_NUMBER: _ClassVar[int]
    TOOL_CALL_ID_FIELD_NUMBER: _ClassVar[int]
    MODEL_FIELD_NUMBER: _ClassVar[int]
    REDACTED_FIELD_NUMBER: _ClassVar[int]
    WITHHELD_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    EXPORTED_AT_FIELD_NUMBER: _ClassVar[int]
    id: str
    analysis_id: str
    organization_id: str
    project_id: str
    actor: TranscriptMessage.Actor
    chat_id: str
    generation: int
    message_id: str
    sequence: int
    role: str
    content: str
    content_bytes: int
    content_truncated: bool
    tool_calls: _containers.RepeatedCompositeFieldContainer[TranscriptMessage.ToolCall]
    tool_call_id: str
    model: str
    redacted: bool
    withheld: bool
    created_at: str
    exported_at: str
    def __init__(self, id: _Optional[str] = ..., analysis_id: _Optional[str] = ..., organization_id: _Optional[str] = ..., project_id: _Optional[str] = ..., actor: _Optional[_Union[TranscriptMessage.Actor, _Mapping]] = ..., chat_id: _Optional[str] = ..., generation: _Optional[int] = ..., message_id: _Optional[str] = ..., sequence: _Optional[int] = ..., role: _Optional[str] = ..., content: _Optional[str] = ..., content_bytes: _Optional[int] = ..., content_truncated: _Optional[bool] = ..., tool_calls: _Optional[_Iterable[_Union[TranscriptMessage.ToolCall, _Mapping]]] = ..., tool_call_id: _Optional[str] = ..., model: _Optional[str] = ..., redacted: _Optional[bool] = ..., withheld: _Optional[bool] = ..., created_at: _Optional[str] = ..., exported_at: _Optional[str] = ...) -> None: ...
