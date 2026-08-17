from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class HookMessage(_message.Message):
    __slots__ = ("id", "chat_id", "project_id", "role", "content", "model", "message_id", "tool_call_id", "user_id", "external_user_id", "finish_reason", "tool_calls", "user_agent", "source", "replayed", "created_at", "session", "hook_source", "adapter", "chat_title", "uncorrelated_prompt", "native_prompt")
    class SessionRef(_message.Message):
        __slots__ = ("session_id", "organization_id", "user_id", "user_email", "user_account_id")
        SESSION_ID_FIELD_NUMBER: _ClassVar[int]
        ORGANIZATION_ID_FIELD_NUMBER: _ClassVar[int]
        USER_ID_FIELD_NUMBER: _ClassVar[int]
        USER_EMAIL_FIELD_NUMBER: _ClassVar[int]
        USER_ACCOUNT_ID_FIELD_NUMBER: _ClassVar[int]
        session_id: str
        organization_id: str
        user_id: str
        user_email: str
        user_account_id: str
        def __init__(self, session_id: _Optional[str] = ..., organization_id: _Optional[str] = ..., user_id: _Optional[str] = ..., user_email: _Optional[str] = ..., user_account_id: _Optional[str] = ...) -> None: ...
    ID_FIELD_NUMBER: _ClassVar[int]
    CHAT_ID_FIELD_NUMBER: _ClassVar[int]
    PROJECT_ID_FIELD_NUMBER: _ClassVar[int]
    ROLE_FIELD_NUMBER: _ClassVar[int]
    CONTENT_FIELD_NUMBER: _ClassVar[int]
    MODEL_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_ID_FIELD_NUMBER: _ClassVar[int]
    TOOL_CALL_ID_FIELD_NUMBER: _ClassVar[int]
    USER_ID_FIELD_NUMBER: _ClassVar[int]
    EXTERNAL_USER_ID_FIELD_NUMBER: _ClassVar[int]
    FINISH_REASON_FIELD_NUMBER: _ClassVar[int]
    TOOL_CALLS_FIELD_NUMBER: _ClassVar[int]
    USER_AGENT_FIELD_NUMBER: _ClassVar[int]
    SOURCE_FIELD_NUMBER: _ClassVar[int]
    REPLAYED_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    SESSION_FIELD_NUMBER: _ClassVar[int]
    HOOK_SOURCE_FIELD_NUMBER: _ClassVar[int]
    ADAPTER_FIELD_NUMBER: _ClassVar[int]
    CHAT_TITLE_FIELD_NUMBER: _ClassVar[int]
    UNCORRELATED_PROMPT_FIELD_NUMBER: _ClassVar[int]
    NATIVE_PROMPT_FIELD_NUMBER: _ClassVar[int]
    id: str
    chat_id: str
    project_id: str
    role: str
    content: str
    model: str
    message_id: str
    tool_call_id: str
    user_id: str
    external_user_id: str
    finish_reason: str
    tool_calls: bytes
    user_agent: str
    source: str
    replayed: bool
    created_at: str
    session: HookMessage.SessionRef
    hook_source: str
    adapter: str
    chat_title: str
    uncorrelated_prompt: bool
    native_prompt: bool
    def __init__(self, id: _Optional[str] = ..., chat_id: _Optional[str] = ..., project_id: _Optional[str] = ..., role: _Optional[str] = ..., content: _Optional[str] = ..., model: _Optional[str] = ..., message_id: _Optional[str] = ..., tool_call_id: _Optional[str] = ..., user_id: _Optional[str] = ..., external_user_id: _Optional[str] = ..., finish_reason: _Optional[str] = ..., tool_calls: _Optional[bytes] = ..., user_agent: _Optional[str] = ..., source: _Optional[str] = ..., replayed: _Optional[bool] = ..., created_at: _Optional[str] = ..., session: _Optional[_Union[HookMessage.SessionRef, _Mapping]] = ..., hook_source: _Optional[str] = ..., adapter: _Optional[str] = ..., chat_title: _Optional[str] = ..., uncorrelated_prompt: _Optional[bool] = ..., native_prompt: _Optional[bool] = ...) -> None: ...
